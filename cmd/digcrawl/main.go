package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"strings"
	"time"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/collect/store"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/version"
	"github.com/originaleric/digcrawl/transport/bridge"
	"github.com/originaleric/digcrawl/transport/collector"
	"github.com/originaleric/digcrawl/transport/devhost"
	httpgw "github.com/originaleric/digcrawl/transport/http"
	mcpgw "github.com/originaleric/digcrawl/transport/mcp"
	stdiogw "github.com/originaleric/digcrawl/transport/stdio"
)

func main() {
	run(os.Args)
}

func run(argv []string) {
	prog := filepath.Base(argv[0])
	if prog == "digcollect" {
		runCollectCommand(argv[1:])
		return
	}
	if len(argv) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch argv[1] {
	case "gateway":
		runGateway(argv[2:])
	case "collector":
		runCollector(argv[2:])
	case "dev-host":
		runDevHost(argv[2:])
	case "mcp":
		runMCP(argv[2:])
	case "bridge":
		runBridge(argv[2:])
	case "stdio":
		runStdio(argv[2:])
	case "collect":
		runCollectCommand(argv[2:])
	case "asr":
		runASR(argv[2:])
	case "video":
		runVideo(argv[2:])
	case "watchlist":
		runWatchlist(argv[2:])
	case "connect":
		runConnect(argv[2:])
	case "login":
		runLogin(argv[2:])
	case "browser":
		runBrowser(argv[2:])
	case "init":
		runInit(argv[2:])
	case "config":
		runConfig(argv[2:])
	case "doctor":
		runDoctor(argv[2:])
	case "version", "--version", "-v":
		printVersion()
	case "help", "-h", "--help":
		printUsage()
	default:
		if looksLikeURL(argv[1]) {
			runCollectCommand(argv[1:])
			return
		}
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", argv[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `DigCrawl — 面向任意 Agent 的本地工具网关

用法:
  digcrawl gateway [flags]     HTTP 工具网关
  digcrawl collector [flags]   WebSocket 反向连接 Collector
  digcrawl mcp [flags]         MCP 服务（stdio，供 IDE 使用）
  digcrawl stdio [flags]       stdin/stdout 逐行 JSON 协议
  digcrawl dev-host [flags]    本地参考宿主（仅开发）
  digcrawl collect <url>       采集 URL 并导出 Markdown/HTML/JSON
  digcrawl collect inbox       处理 collect-inbox/ 中的链接文件
  digcrawl collect list        列出 SQLite 采集记录（需 PersistMode=sqlite/both）
  digcrawl collect show <id>   查看单条采集详情（id 或 URL）
  digcrawl collect migrate-index  从 index.json 导入 SQLite
  digcrawl asr transcribe <video>  ffmpeg 抽音频并转写
  digcrawl asr check [--config <profile>] [--scope auto|root|skill]  检查所选本地 ASR 模型
  digcrawl video <path-or-url>   解析本地视频或直接视频 URL（ASR/OCR）
  digcrawl watchlist run [--once]  定时抓取 watchlist 并通知
  digcrawl <url>               等价于 digcrawl collect <url>
  digcrawl connect [flags]     保存宿主配置并启动 Collector
  digcrawl login [flags]       保存 Collector Token 到配置
  digcrawl browser check --platform xiaohongshu|douyin|toutiao|threads|instagram [--url URL] [--trial]
  digcrawl init --profile skill [--force]
  digcrawl config validate --config <profile>
  digcrawl doctor --config <profile>
  digcrawl version
  digcrawl help

采集示例:
  digcrawl collect "https://mp.weixin.qq.com/s/..."
  digcrawl collect --file urls.txt --format md,html,json --out ./output
  digcrawl collect inbox --watch

完整文档: USAGE.md
宿主 SDK: github.com/originaleric/digcrawl/client

`)
}

func printVersion() {
	fmt.Printf("%s %s\ncommit: %s\nbuilt: %s\n", version.RuntimeName, version.RuntimeVersion, version.Commit, version.BuildDate)
}

func looksLikeURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func runGateway(args []string) {
	fs := flag.NewFlagSet("gateway", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	addr := fs.String("addr", "", "listen address (overrides Gateway.ListenAddr)")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	listen := cfg.Gateway.ListenAddr
	if *addr != "" {
		listen = *addr
	}
	if listen == "" {
		listen = ":8787"
	}

	rt, err := runtime.NewRuntime(cfg)
	if err != nil {
		log.Fatalf("runtime: %v", err)
	}
	srv := httpgw.NewServer(rt, rt.ArtifactStore(), cfg.Gateway.AuthToken)
	srv.EnableWatchlistAdmin(cfg)
	log.Printf("DigCrawl HTTP gateway listening on %s (instance=%s)", listen, cfg.Gateway.InstanceID)
	if err := srv.ListenAndServe(listen); err != nil {
		log.Fatalf("gateway server: %v", err)
	}
}

func runCollector(args []string) {
	fs := flag.NewFlagSet("collector", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	server := fs.String("server", "", "host base URL (overrides Collector.ServerURL)")
	token := fs.String("token", "", "auth token (overrides Collector.Token)")
	instanceID := fs.String("instance-id", "", "collector instance id")
	pullSec := fs.Int("pull-interval", -1, "pull interval seconds; -1 uses config")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *server != "" {
		cfg.Collector.ServerURL = *server
	}
	if *token != "" {
		cfg.Collector.Token = *token
	}
	if *instanceID != "" {
		cfg.Collector.InstanceID = *instanceID
	}
	if *pullSec >= 0 {
		cfg.Collector.PullIntervalSec = *pullSec
	}

	opts := collector.OptionsFromConfig(cfg)
	if opts.ServerURL == "" {
		log.Fatal("collector: --server or Collector.ServerURL is required")
	}

	rt, err := runtime.NewCollectorRuntime(cfg)
	if err != nil {
		log.Fatalf("runtime: %v", err)
	}
	client := collector.NewClient(opts, rt)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("DigCrawl collector connecting to %s instance=%s pull=%s",
		opts.ServerURL, opts.InstanceID, opts.PullInterval)
	if err := client.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("collector: %v", err)
	}
}

func runDevHost(args []string) {
	fs := flag.NewFlagSet("dev-host", flag.ExitOnError)
	addr := fs.String("addr", ":8790", "listen address")
	token := fs.String("token", "dev", "auth token for collectors")
	wsPath := fs.String("ws-path", "/digcrawl/v1/collector/ws", "WebSocket path")
	_ = fs.Parse(args)

	srv := devhost.NewServer(*token, *wsPath)
	log.Printf("DigCrawl dev-host (reference only — implement production host in your project)")
	if err := srv.ListenAndServe(*addr); err != nil {
		log.Fatalf("dev-host: %v", err)
	}
}

func runMCP(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	configPath := fs.String("config", "", "path to profile (default: user skill profile)")
	idleTimeout := fs.Duration("idle-timeout", mcpgw.DefaultStdioIdleTimeout, "exit after this much inactive stdio time (explicit 0 disables recovery)")
	_ = fs.Parse(args)
	idleTimeoutExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "idle-timeout" {
			idleTimeoutExplicit = true
		}
	})
	if *idleTimeout < 0 {
		log.Fatal("mcp: --idle-timeout must be zero or positive")
	}

	profilePath := profilePathForMCP(*configPath)
	cfg, err := loadRequiredConfig(profilePath)
	if err != nil {
		log.Fatalf("load MCP profile: %v (run `digcrawl init --profile skill` or pass --config)", err)
	}
	allowDefaultSkillProfileTools(cfg)
	bridgeIdleTimeout := bridge.DefaultIdleTimeout
	if idleTimeoutExplicit && *idleTimeout > 0 {
		bridgeIdleTimeout = *idleTimeout
	}
	startupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt, err := bridge.NewExecutor(startupCtx, bridgeExecutable(), profilePath, cfg, bridgeIdleTimeout)
	if err != nil {
		log.Fatalf("start local bridge: %v", err)
	}
	log.Printf("DigCrawl MCP server on stdio (instance=%s)", cfg.Gateway.InstanceID)
	var serveErr error
	if idleTimeoutExplicit {
		serveErr = mcpgw.ServeToolRuntimeStdio(rt, *idleTimeout, false)
	} else {
		serveErr = mcpgw.ServeToolRuntimeStdio(rt, mcpgw.DefaultStdioIdleTimeout, true)
	}
	if serveErr != nil {
		log.Fatalf("mcp: %v", serveErr)
	}
}

func bridgeExecutable() string {
	if executable := strings.TrimSpace(os.Getenv("DIGCRAWL_BRIDGE_EXECUTABLE")); executable != "" {
		return executable
	}
	return os.Args[0]
}

// allowDefaultSkillProfileTools keeps existing standard Skill profiles
// forward-compatible without rewriting their allowlist at MCP startup.
func allowDefaultSkillProfileTools(cfg *config.Config) {
	if cfg == nil || !config.IsDefaultSkillProfile(cfg.SourcePath) || !isLegacyDefaultSkillAllowlist(cfg.Gateway.AllowedTools) {
		return
	}
	for _, required := range []string{"skill.asr.enable", "youtube.video.read", "skill.media.configure"} {
		found := false
		for _, tool := range cfg.Gateway.AllowedTools {
			if tool == required {
				found = true
				break
			}
		}
		if !found {
			cfg.Gateway.AllowedTools = append(cfg.Gateway.AllowedTools, required)
		}
	}
}

// isLegacyDefaultSkillAllowlist recognizes only frozen shipped allowlists that
// predate the current configuration tool. A default-path profile may still be
// an operator-customized policy and must not be broadened merely because of
// where it is stored.
func isLegacyDefaultSkillAllowlist(tools []string) bool {
	return sameToolSet(tools, legacySkillReadTools) ||
		sameToolSet(tools, append([]string{"skill.asr.enable"}, legacySkillReadTools...)) ||
		sameToolSet(tools, preSkillMediaConfigureTools)
}

func sameToolSet(actual, standard []string) bool {
	want := make(map[string]struct{}, len(standard))
	for _, tool := range standard {
		want[tool] = struct{}{}
	}
	if len(actual) != len(want) {
		return false
	}
	seen := make(map[string]struct{}, len(actual))
	for _, tool := range actual {
		if _, ok := want[tool]; !ok {
			return false
		}
		if _, duplicate := seen[tool]; duplicate {
			return false
		}
		seen[tool] = struct{}{}
	}
	return true
}

func runCollectCommand(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "inbox":
			runCollectInbox(args[1:])
			return
		case "list":
			runCollectList(args[1:])
			return
		case "show":
			runCollectShow(args[1:])
			return
		case "migrate-index":
			runCollectMigrateIndex(args[1:])
			return
		}
	}
	runCollect(args)
}

func runCollect(args []string) {
	fs := flag.NewFlagSet("collect", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	outDir := fs.String("out", "", "output directory (overrides Collect.OutputDir)")
	format := fs.String("format", "", "export formats: md,html,json (comma-separated)")
	urlFile := fs.String("file", "", "text file with one URL per line")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *outDir != "" {
		cfg.Collect.OutputDir = *outDir
	}

	formats := parseFormats(*format, cfg.Collect.DefaultFormats)

	svc, err := collect.NewService(cfg)
	if err != nil {
		log.Fatalf("collect service: %v", err)
	}

	ctx := context.Background()

	if *urlFile != "" {
		results, err := svc.CollectFile(ctx, *urlFile, formats, cfg.Collect.OutputDir)
		logCollectResults(results)
		if err != nil {
			log.Fatalf("collect file: %v", err)
		}
		return
	}

	if len(fs.Args()) == 0 {
		log.Fatal("collect: provide a URL, --file, or `digcrawl collect inbox`")
	}
	url := fs.Args()[0]
	result, err := svc.CollectURL(ctx, url, formats, cfg.Collect.OutputDir)
	if err != nil {
		log.Fatalf("collect: %v", err)
	}
	logCollectResults([]collect.ExportResult{result})
}

func runCollectInbox(args []string) {
	fs := flag.NewFlagSet("collect inbox", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	outDir := fs.String("out", "", "output directory (overrides Collect.OutputDir)")
	format := fs.String("format", "", "export formats: md,html,json (comma-separated)")
	watch := fs.Bool("watch", false, "poll inbox until interrupted")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *outDir != "" {
		cfg.Collect.OutputDir = *outDir
	}
	formats := parseFormats(*format, cfg.Collect.DefaultFormats)

	svc, err := collect.NewService(cfg)
	if err != nil {
		log.Fatalf("collect service: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *watch {
		log.Printf("watching inbox %s (Ctrl+C to stop)", cfg.Collect.InboxDir)
		if err := svc.WatchInbox(ctx, formats, cfg.Collect.OutputDir); err != nil && err != context.Canceled {
			log.Fatalf("collect inbox watch: %v", err)
		}
		return
	}

	results, err := svc.ProcessInbox(ctx, formats, cfg.Collect.OutputDir)
	logCollectResults(results)
	if err != nil {
		log.Fatalf("collect inbox: %v", err)
	}
	if len(results) == 0 {
		log.Printf("inbox empty: drop .url or .txt files into %s", cfg.Collect.InboxDir)
	}
}

func logCollectResults(results []collect.ExportResult) {
	for _, r := range results {
		for _, w := range r.Written {
			log.Printf("wrote %s", w)
		}
	}
}

func runCollectList(args []string) {
	fs := flag.NewFlagSet("collect list", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	limit := fs.Int("limit", 20, "max rows")
	offset := fs.Int("offset", 0, "offset")
	query := fs.String("q", "", "search query")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx := context.Background()
	var rows []store.CollectRecord
	if strings.TrimSpace(*query) != "" {
		rows, err = collect.SearchCollects(ctx, cfg, *query, *limit)
	} else {
		rows, err = collect.ListCollects(ctx, cfg, *limit, *offset)
	}
	if err != nil {
		log.Fatalf("collect list: %v", err)
	}
	if len(rows) == 0 {
		log.Printf("no collects found")
		return
	}
	for _, r := range rows {
		fmt.Printf("%s\t%s\t%s\t%s\n", r.ID, r.Platform, r.Title, r.SourceURL)
	}
}

func runCollectShow(args []string) {
	fs := flag.NewFlagSet("collect show", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	_ = fs.Parse(args)
	if len(fs.Args()) == 0 {
		log.Fatal("collect show: provide collect id or URL")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	detail, err := collect.ShowCollect(context.Background(), cfg, fs.Args()[0])
	if err != nil {
		log.Fatalf("collect show: %v", err)
	}
	if detail == nil {
		log.Fatal("collect not found")
	}
	data, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	fmt.Println(string(data))
}

func runCollectMigrateIndex(args []string) {
	fs := flag.NewFlagSet("collect migrate-index", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	outDir := fs.String("out", "", "output directory (overrides Collect.OutputDir)")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *outDir != "" {
		cfg.Collect.OutputDir = *outDir
	}
	n, err := collect.MigrateIndexToSQLite(context.Background(), cfg, cfg.Collect.OutputDir)
	if err != nil {
		log.Fatalf("migrate-index: %v", err)
	}
	log.Printf("imported %d entries into %s", n, cfg.Collect.DBPathOrDefault())
}

func parseFormats(flagVal string, defaults []string) []string {
	if strings.TrimSpace(flagVal) == "" {
		return defaults
	}
	var out []string
	for _, p := range strings.Split(flagVal, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runStdio(args []string) {
	fs := flag.NewFlagSet("stdio", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	rt, err := runtime.NewRuntime(cfg)
	if err != nil {
		log.Fatalf("runtime: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := stdiogw.NewServer(rt).Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("stdio: %v", err)
	}
}

func runConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	configPath := fs.String("config", "", "config path (default ~/.digcrawl/config.yaml)")
	server := fs.String("server", "", "host base URL (required)")
	token := fs.String("token", "", "collector auth token")
	saveOnly := fs.Bool("save-only", false, "only save config, do not start collector")
	_ = fs.Parse(args)

	if *server == "" {
		log.Fatal("connect: --server is required (e.g. https://your-host.example.com)")
	}

	path := *configPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	cfg, err := loadConfig(path)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.Collector.ServerURL = *server
	if *token != "" {
		cfg.Collector.Token = *token
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("mkdir config: %v", err)
	}
	if err := config.Save(path, cfg); err != nil {
		log.Fatalf("save config: %v", err)
	}
	log.Printf("saved collector settings to %s (server=%s)", path, cfg.Collector.ServerURL)
	if *saveOnly {
		return
	}
	runCollector([]string{"--config", path})
}

func runLogin(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	configPath := fs.String("config", "", "config path (default ~/.digcrawl/config.yaml)")
	token := fs.String("token", "", "collector auth token to save")
	server := fs.String("server", "", "optional host base URL")
	_ = fs.Parse(args)

	path := *configPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	cfg, err := loadConfig(path)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *token != "" {
		cfg.Collector.Token = *token
	}
	if *server != "" {
		cfg.Collector.ServerURL = *server
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("mkdir config: %v", err)
	}
	if err := config.Save(path, cfg); err != nil {
		log.Fatalf("save config: %v", err)
	}
	log.Printf("updated %s — run: digcrawl connect --server <host-url>", path)
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		return config.Get(), nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			log.Printf("config file %q not found, using defaults", path)
			return config.Get(), nil
		}
		return nil, err
	}
	return config.Load(path)
}
