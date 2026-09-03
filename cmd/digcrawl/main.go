package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/collect/store"
	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/asr"
	"github.com/originaleric/digcrawl/internal/platform"
	"github.com/originaleric/digcrawl/internal/platform/wechatchannels"
	"github.com/originaleric/digcrawl/runtime"
	"github.com/originaleric/digcrawl/runtime/version"
	"github.com/originaleric/digcrawl/transport/collector"
	"github.com/originaleric/digcrawl/transport/devhost"
	httpgw "github.com/originaleric/digcrawl/transport/http"
	mcpgw "github.com/originaleric/digcrawl/transport/mcp"
	stdiogw "github.com/originaleric/digcrawl/transport/stdio"
)

const (
	defaultWechatChannelsDownloadTimeout  = 30 * time.Minute
	defaultWechatChannelsDownloadMaxBytes = wechatchannels.DefaultDownloadMaxBytes
	wechatChannelsCaptureStateRoot        = "storage/digcrawl/wechat-channels-captures"
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
	case "stdio":
		runStdio(argv[2:])
	case "collect":
		runCollectCommand(argv[2:])
	case "wechat-channels":
		runWechatChannels(argv[2:])
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
  digcrawl wechat-channels bridge start  启动 macOS 微信视频号本地桥接并唤起微信
  digcrawl wechat-channels bridge stop   停止桥接并恢复系统代理/移除本地证书
  digcrawl wechat-channels import        导入桥接捕获的当前视频号卡片
  digcrawl wechat-channels download      下载页面适配器捕获的当前视频
  digcrawl wechat-channels capture       单终端启动桥接、下载当前视频并可选转写
  digcrawl wechat-channels inspect       只读检查微信是否已暴露 CDP 调试通道
  digcrawl asr setup [--yes]       安装隔离的本地 faster-whisper 运行时
  digcrawl asr transcribe <file>   ffmpeg 抽取并转写本地视频或音频
  digcrawl video <path-or-url>     解析本地视频或直接视频 URL（ASR/OCR）
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

func runWechatChannels(args []string) {
	if len(args) == 0 {
		log.Fatal("wechat-channels: use `digcrawl wechat-channels capture`, `bridge start`, `bridge stop`, `import`, `download`, or `inspect`")
	}
	switch args[0] {
	case "import":
		runWechatChannelsImport(args[1:])
	case "bridge":
		runWechatChannelsBridge(args[1:])
	case "download":
		runWechatChannelsDownload(args[1:])
	case "capture":
		runWechatChannelsCapture(args[1:])
	case "inspect":
		runWechatChannelsInspect(args[1:])
	default:
		log.Fatal("wechat-channels: use `digcrawl wechat-channels capture`, `bridge start`, `bridge stop`, `import`, `download`, or `inspect`")
	}
}

func runWechatChannelsInspect(args []string) {
	fs := flag.NewFlagSet("wechat-channels inspect", flag.ExitOnError)
	endpoint := fs.String("endpoint", "", "explicit existing loopback CDP endpoint (default: inspect WeChat's loopback listeners)")
	_ = fs.Parse(args)
	targets, err := wechatchannels.InspectDesktop(context.Background(), *endpoint)
	if err != nil {
		if errors.Is(err, wechatchannels.ErrDesktopDebugUnavailable) {
			fmt.Fprintln(os.Stdout, "WeChat has no accessible CDP endpoint. DigCrawl did not change WeChat, its proxy, or its launch options.")
			return
		}
		log.Fatalf("inspect WeChat desktop CDP: %v", err)
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stdout, "WeChat exposes CDP, but no Channels page target is currently open.")
		return
	}
	for _, target := range targets {
		fmt.Fprintf(os.Stdout, "Type: %s\nTitle: %s\nURL: %s\n\n", target.Type, target.Title, target.URL)
	}
}

func runWechatChannelsBridge(args []string) {
	if len(args) == 0 {
		log.Fatal("wechat-channels bridge: use `start` or `stop`")
	}
	switch args[0] {
	case "start":
		runWechatChannelsBridgeStart(args[1:])
	case "stop":
		runWechatChannelsBridgeStop(args[1:])
	default:
		log.Fatal("wechat-channels bridge: use `start` or `stop`")
	}
}

func wechatChannelsBridgeOptions(fs *flag.FlagSet) (*int, *string, *string, *string) {
	port := fs.Int("port", wechatchannels.DefaultBridgePort, "loopback proxy and bridge port")
	stateDir := fs.String("state-dir", "", "bridge certificate and recovery-state directory")
	networkService := fs.String("network-service", "", "macOS network service to proxy (auto-detected by default)")
	wechatApp := fs.String("wechat-app", "/Applications/WeChat.app", "path to the macOS WeChat app")
	return port, stateDir, networkService, wechatApp
}

func runWechatChannelsBridgeStart(args []string) {
	fs := flag.NewFlagSet("wechat-channels bridge start", flag.ExitOnError)
	port, stateDir, networkService, wechatApp := wechatChannelsBridgeOptions(fs)
	noOpenWechat := fs.Bool("no-open-wechat", false, "start the bridge without launching WeChat")
	capturePageHTML := fs.String("capture-page-html", "", "diagnostic only: write the first original Channels HTML to this absolute local path (may contain sensitive data)")
	captureMetadata := fs.String("capture-metadata", "", "diagnostic only: write safe candidate request metadata as local NDJSON to this absolute path")
	pageAdapter := fs.Bool("page-adapter", false, "enable the explicit same-origin Channels page adapter required for video download")
	yes := fs.Bool("yes", false, "confirm certificate installation and temporary system proxy changes")
	_ = fs.Parse(args)
	if !*yes {
		confirmed, err := confirmWechatChannelsBridge(os.Stdin, os.Stderr, *port)
		if err != nil {
			log.Fatalf("confirm bridge start: %v", err)
		}
		if !confirmed {
			log.Printf("wechat channels bridge start cancelled")
			return
		}
	}
	bridge, err := wechatchannels.NewBridge(wechatchannels.BridgeOptions{Port: *port, StateDir: *stateDir, NetworkService: *networkService, WeChatAppPath: *wechatApp, PageHTMLCapturePath: *capturePageHTML, DiagnosticMetadataPath: *captureMetadata, EnablePageAdapter: *pageAdapter})
	if err != nil {
		log.Fatalf("configure WeChat Channels bridge: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bridge.Start(ctx, !*noOpenWechat); err != nil {
		log.Fatalf("start WeChat Channels bridge: %v", err)
	}
	if *capturePageHTML != "" {
		fmt.Fprintf(os.Stderr, "Diagnostic page HTML capture is enabled. The first original Channels page will be saved locally to %s with mode 0600; it may contain sensitive page state and is not uploaded.\n", *capturePageHTML)
	}
	if *captureMetadata != "" {
		fmt.Fprintf(os.Stderr, "Diagnostic metadata capture is enabled. Candidate request metadata will be written locally as NDJSON to %s with mode 0600. It contains only host, path without query, method, response status, content type, content length, and transport protocol; it never records cookies, request bodies, or response bodies.\n", *captureMetadata)
	}
	if *pageAdapter {
		fmt.Fprintln(os.Stderr, "The page adapter is enabled. It rewrites only the QQ Channels JavaScript module graph and keeps the current video's playback URL and optional decode key in bridge memory for one explicit local download. It does not export cookies, request bodies, or response payloads.")
	}
	fmt.Fprintf(os.Stdout, "WeChat Channels bridge is running on 127.0.0.1:%d. Open and play the desired video card in WeChat, then run `digcrawl wechat-channels import`", *port)
	if *pageAdapter {
		fmt.Fprint(os.Stdout, " or `digcrawl wechat-channels download --out /absolute/path/video.mp4`")
	}
	fmt.Fprintln(os.Stdout, ". Press Ctrl-C to restore the proxy and remove the certificate.")
	select {
	case <-ctx.Done():
	case <-bridge.StopRequested():
	}
	if err := bridge.Stop(context.Background()); err != nil {
		log.Printf("stop WeChat Channels bridge: %v", err)
	}
}

func runWechatChannelsBridgeStop(args []string) {
	fs := flag.NewFlagSet("wechat-channels bridge stop", flag.ExitOnError)
	port, stateDir, networkService, wechatApp := wechatChannelsBridgeOptions(fs)
	_ = fs.Parse(args)
	bridge, err := wechatchannels.NewBridge(wechatchannels.BridgeOptions{Port: *port, StateDir: *stateDir, NetworkService: *networkService, WeChatAppPath: *wechatApp})
	if err != nil {
		log.Fatalf("configure WeChat Channels bridge: %v", err)
	}
	if err := bridge.Stop(context.Background()); err != nil {
		log.Fatalf("stop WeChat Channels bridge: %v", err)
	}
	fmt.Fprintln(os.Stdout, "WeChat Channels bridge stopped; this bridge's proxy settings and certificate were removed when still owned by DigCrawl.")
}

func confirmWechatChannelsBridge(in io.Reader, out io.Writer, port int) (bool, error) {
	fmt.Fprintf(out, "This installs a per-user local CA and temporarily enables a PAC: only *.qq.com goes to 127.0.0.1:%d; all other traffic keeps the current manual proxy path. It MITMs only qq.com and retains only current-video metadata in memory unless --page-adapter is selected. Continue? [y/N]: ", port)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func runWechatChannelsDownload(args []string) {
	fs := flag.NewFlagSet("wechat-channels download", flag.ExitOnError)
	out := fs.String("out", "", "absolute output path for the downloaded video")
	bridgeURL := fs.String("bridge-url", wechatchannels.DefaultBridgeURL, "user-authorized local WeChat bridge URL (loopback only)")
	bridgeStateDir := fs.String("bridge-state-dir", "", "bridge recovery-state directory (must match bridge start)")
	downloadTimeout := fs.Duration("download-timeout", defaultWechatChannelsDownloadTimeout, "maximum total time for the video download")
	maxBytes := fs.Int64("max-bytes", defaultWechatChannelsDownloadMaxBytes, "maximum downloaded video size in bytes")
	yes := fs.Bool("yes", false, "confirm download without an interactive prompt")
	_ = fs.Parse(args)
	if !filepath.IsAbs(strings.TrimSpace(*out)) {
		log.Fatal("wechat-channels download: --out must be an absolute output path")
	}
	if *downloadTimeout <= 0 {
		log.Fatal("wechat-channels download: --download-timeout must be greater than zero")
	}
	if *maxBytes <= 0 {
		log.Fatal("wechat-channels download: --max-bytes must be greater than zero")
	}
	if err := wechatchannels.CheckDownloadBridgeCompatibility(*bridgeStateDir); err != nil {
		log.Fatalf("wechat-channels download: %v", err)
	}
	if !*yes {
		confirmed, err := confirmWechatChannelsDownload(os.Stdin, os.Stderr, *out)
		if err != nil {
			log.Fatalf("confirm video download: %v", err)
		}
		if !confirmed {
			log.Printf("wechat channels video download cancelled")
			return
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	downloadCtx, cancelDownload := context.WithTimeout(ctx, *downloadTimeout)
	defer cancelDownload()
	result, err := wechatchannels.DownloadActiveFromState(downloadCtx, *bridgeURL, *bridgeStateDir, *out, *maxBytes)
	if err != nil {
		if errors.Is(downloadCtx.Err(), context.DeadlineExceeded) {
			log.Fatalf("download current WeChat Channels video: timed out after %s; no partial output was published", *downloadTimeout)
		}
		log.Fatalf("download current WeChat Channels video: %v", err)
	}
	fmt.Fprintf(os.Stdout, "Downloaded current WeChat Channels video to %s (%d bytes", result.OutputPath, result.Bytes)
	if result.Decrypted {
		fmt.Fprint(os.Stdout, ", decrypted")
	}
	fmt.Fprintln(os.Stdout, ").")
}

// runWechatChannelsCapture is the single-terminal workflow for an already
// authorized desktop WeChat session. It deliberately leaves WeChat running:
// the user only reopens and plays the card after the PAC is active.
func runWechatChannelsCapture(args []string) {
	fs := flag.NewFlagSet("wechat-channels capture", flag.ExitOnError)
	port, stateDir, networkService, wechatApp := wechatChannelsBridgeOptions(fs)
	out := fs.String("out", "", "optional absolute output path for the downloaded video (autogenerated under local captures when omitted)")
	transcribe := fs.Bool("transcribe", false, "transcribe the downloaded local video using the configured ASR provider")
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml used only with --transcribe")
	wait := fs.Duration("wait", 10*time.Minute, "maximum time to wait for page-adapter video readiness before cleanup")
	downloadTimeout := fs.Duration("download-timeout", defaultWechatChannelsDownloadTimeout, "maximum total time for the video download after its resource is ready")
	maxBytes := fs.Int64("max-bytes", defaultWechatChannelsDownloadMaxBytes, "maximum downloaded video size in bytes")
	yes := fs.Bool("yes", false, "confirm the temporary QQ-only proxy and local video download without an interactive prompt")
	_ = fs.Parse(args)
	outputPath := strings.TrimSpace(*out)
	if outputPath != "" {
		if !filepath.IsAbs(outputPath) {
			log.Fatal("wechat-channels capture: --out must be an absolute output path")
		}
		outputPath = filepath.Clean(outputPath)
		if _, err := os.Lstat(outputPath); err == nil {
			log.Fatalf("wechat-channels capture: output already exists: %s", outputPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			log.Fatalf("wechat-channels capture: inspect output path: %v", err)
		}
		outputDir := filepath.Dir(outputPath)
		info, err := os.Stat(outputDir)
		if err != nil {
			log.Fatalf("wechat-channels capture: inspect output directory: %v", err)
		}
		if !info.IsDir() {
			log.Fatalf("wechat-channels capture: output parent is not a directory: %s", outputDir)
		}
	}
	if *wait <= 0 {
		log.Fatal("wechat-channels capture: --wait must be greater than zero")
	}
	if *downloadTimeout <= 0 {
		log.Fatal("wechat-channels capture: --download-timeout must be greater than zero")
	}
	if *maxBytes <= 0 {
		log.Fatal("wechat-channels capture: --max-bytes must be greater than zero")
	}
	var err error
	var asrCfg config.ASRConfig
	if *transcribe {
		cfg, err := loadConfig(*configPath)
		if err != nil {
			log.Fatalf("load config for local video transcription: %v", err)
		}
		if err := asr.CheckAvailable(cfg.ASR); err != nil {
			log.Fatalf("check configured ASR before capture: %v", err)
		}
		asrCfg = cfg.ASR
	}
	destination := outputPath
	if destination == "" {
		destination = "an automatically named MP4 under " + wechatChannelsCaptureStateRoot
	}
	if !*yes {
		confirmed, err := confirmWechatChannelsCapture(os.Stdin, os.Stderr, destination, *transcribe, asrCfg.ProviderOrDefault())
		if err != nil {
			log.Fatalf("confirm WeChat Channels capture: %v", err)
		}
		if !confirmed {
			log.Printf("wechat channels capture cancelled")
			return
		}
	}
	if outputPath == "" {
		outputPath, err = newWechatChannelsCaptureOutputPath(wechatChannelsCaptureStateRoot, time.Now())
		if err != nil {
			log.Fatalf("create WeChat Channels capture output path: %v", err)
		}
	}
	captureStateDir := strings.TrimSpace(*stateDir)
	autoStateDir := captureStateDir == ""
	if autoStateDir {
		captureStateDir, err = newWechatChannelsCaptureStateDir(wechatChannelsCaptureStateRoot, time.Now())
		if err != nil {
			log.Fatalf("create WeChat Channels capture state directory: %v", err)
		}
	} else {
		captureStateDir = filepath.Clean(captureStateDir)
	}

	bridge, err := wechatchannels.NewBridge(wechatchannels.BridgeOptions{
		Port:              *port,
		StateDir:          captureStateDir,
		NetworkService:    *networkService,
		WeChatAppPath:     *wechatApp,
		EnablePageAdapter: true,
	})
	if err != nil {
		log.Fatalf("configure WeChat Channels capture: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bridge.Start(ctx, false); err != nil {
		log.Fatalf("start WeChat Channels capture: %v", err)
	}
	failed := false
	recoveryCommand := fmt.Sprintf("digcrawl wechat-channels bridge stop --state-dir %q", captureStateDir)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := bridge.Stop(cleanupCtx); err != nil {
			log.Printf("stop WeChat Channels capture: %v; run `%s` to retry restoration", err, recoveryCommand)
			failed = true
		} else if autoStateDir {
			if err := os.RemoveAll(captureStateDir); err != nil {
				log.Printf("remove completed WeChat Channels capture session directory %s: %v; remove it manually", captureStateDir, err)
				failed = true
			}
		}
		if failed {
			os.Exit(1)
		}
	}()
	if err := wechatchannels.CheckDownloadBridgeCompatibility(captureStateDir); err != nil {
		log.Printf("check WeChat Channels bridge download compatibility: %v", err)
		failed = true
		return
	}

	fmt.Fprintf(os.Stdout, "Bridge is ready (session state: %s; output: %s). Keep WeChat open and play one video card; DigCrawl will download it automatically when the page adapter is ready (waiting up to %s). Press Ctrl-C at any time to restore the proxy.\n", captureStateDir, outputPath, *wait)
	bridgeURL := fmt.Sprintf("http://127.0.0.1:%d", *port)
	waitCtx, cancelWait := context.WithTimeout(ctx, *wait)
	defer cancelWait()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		ready, err := wechatchannels.ActiveDownloadReadyFromState(waitCtx, bridgeURL, captureStateDir)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				log.Printf("wait for a downloadable WeChat Channels video: timed out after %s; reopen and play one card, then run capture again", *wait)
				failed = true
				return
			}
			log.Printf("check current WeChat Channels video readiness: %v", err)
			failed = true
			return
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-waitCtx.Done():
			log.Printf("wait for a downloadable WeChat Channels video: timed out after %s; reopen and play one card, then run capture again", *wait)
			failed = true
			return
		case <-ticker.C:
		}
	}
	fmt.Fprintln(os.Stdout, "Video resource detected; downloading it now.")
	downloadCtx, cancelDownload := context.WithTimeout(ctx, *downloadTimeout)
	defer cancelDownload()
	downloaded, err := wechatchannels.DownloadActiveFromState(downloadCtx, bridgeURL, captureStateDir, outputPath, *maxBytes)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		if errors.Is(downloadCtx.Err(), context.DeadlineExceeded) {
			log.Printf("download current WeChat Channels video: timed out after %s; no partial output was published", *downloadTimeout)
		} else {
			log.Printf("download current WeChat Channels video: %v", err)
		}
		failed = true
		return
	}
	fmt.Fprintf(os.Stdout, "Downloaded current WeChat Channels video to %s (%d bytes", downloaded.OutputPath, downloaded.Bytes)
	if downloaded.Decrypted {
		fmt.Fprint(os.Stdout, ", decrypted")
	}
	fmt.Fprintln(os.Stdout, ").")
	if !*transcribe {
		return
	}
	transcript, err := asr.Transcribe(ctx, downloaded.OutputPath, asrCfg, asr.Options{})
	if err != nil {
		log.Printf("transcribe downloaded WeChat Channels video: %v", err)
		failed = true
		return
	}
	encoded, err := json.MarshalIndent(transcript, "", "  ")
	if err != nil {
		log.Printf("encode local video transcription: %v", err)
		failed = true
		return
	}
	fmt.Println(string(encoded))
}

// newWechatChannelsCaptureStateDir creates an isolated recovery-state
// directory for one foreground capture. The timestamp aids local diagnosis;
// MkdirTemp supplies collision-resistant randomness and mode 0700.
func newWechatChannelsCaptureStateDir(root string, now time.Time) (string, error) {
	captureRoot, err := absoluteWechatChannelsCaptureRoot(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(captureRoot, 0o700); err != nil {
		return "", err
	}
	prefix := now.UTC().Format("20060102T150405.000000000Z") + "-"
	return os.MkdirTemp(captureRoot, prefix)
}

// newWechatChannelsCaptureOutputPath reserves a collision-resistant name for
// a capture output without leaving a file that the atomic downloader would
// reject. The downloader still performs the final no-overwrite check.
func newWechatChannelsCaptureOutputPath(root string, now time.Time) (string, error) {
	captureRoot, err := absoluteWechatChannelsCaptureRoot(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(captureRoot, 0o700); err != nil {
		return "", err
	}
	prefix := "wechat_video_" + now.UTC().Format("20060102T150405.000000000Z") + "-*.mp4"
	temporary, err := os.CreateTemp(captureRoot, prefix)
	if err != nil {
		return "", err
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

// absoluteWechatChannelsCaptureRoot makes every automatically generated path
// suitable for the bridge's absolute-path-only download API. The capture
// root is relative to the user's working directory only at the CLI boundary;
// no relative path crosses into the bridge session.
func absoluteWechatChannelsCaptureRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("WeChat Channels capture root is empty")
	}
	return filepath.Abs(root)
}

func confirmWechatChannelsCapture(in io.Reader, out io.Writer, destination string, transcribe bool, provider string) (bool, error) {
	action := "download"
	if transcribe {
		action = "download and transcribe locally"
		if provider != "local" {
			action = "download and transcribe using the configured " + provider + " provider; audio may leave this computer"
		}
	}
	fmt.Fprintf(out, "Temporarily route only QQ traffic through the local bridge, then %s the video you open in WeChat to %s? WeChat will not be restarted; Ctrl-C restores the proxy. Continue? [y/N]: ", action, destination)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func confirmWechatChannelsDownload(in io.Reader, out io.Writer, destination string) (bool, error) {
	fmt.Fprintf(out, "Download the currently open WeChat Channels video to %s? The playback URL and any decode key stay in the running local bridge and will not be written to metadata exports. [y/N]: ", destination)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func runWechatChannelsImport(args []string) {
	fs := flag.NewFlagSet("wechat-channels import", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	outDir := fs.String("out", "", "output directory (overrides Collect.OutputDir)")
	format := fs.String("format", "", "export formats: md,html,json (comma-separated)")
	bridgeURL := fs.String("bridge-url", wechatchannels.DefaultBridgeURL, "user-authorized local WeChat bridge URL (loopback only)")
	bridgeStateDir := fs.String("bridge-state-dir", "", "bridge recovery-state directory (must match bridge start)")
	yes := fs.Bool("yes", false, "confirm import without an interactive prompt")
	_ = fs.Parse(args)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *outDir != "" {
		cfg.Collect.OutputDir = *outDir
	}

	ctx := context.Background()
	content, err := wechatchannels.ImportActiveFromState(ctx, *bridgeURL, *bridgeStateDir)
	if err != nil {
		log.Fatalf("read active WeChat Channels video: %v", err)
	}
	printWechatChannelsPreview(os.Stdout, content)
	if !*yes {
		confirmed, err := confirmWechatChannelsImport(os.Stdin, os.Stderr)
		if err != nil {
			log.Fatalf("confirm import: %v", err)
		}
		if !confirmed {
			log.Printf("wechat channels import cancelled")
			return
		}
	}

	formats := parseFormats(*format, cfg.Collect.DefaultFormats)
	out := content.ToMap()
	result, err := collect.Export(ctx, out, collect.ExportOptions{
		OutputDir: cfg.Collect.OutputDir,
		Formats:   formats,
		// Importing a card never downloads video, cover, or other media. Those
		// require a separately approved capability and an explicit user action.
		DownloadMedia: false,
	})
	if err != nil {
		log.Fatalf("export WeChat Channels import: %v", err)
	}
	if err := persistWechatChannelsImport(ctx, cfg, content.SourceURL, out, &result); err != nil {
		logCollectResults([]collect.ExportResult{result})
		log.Fatalf("persist WeChat Channels import: %v", err)
	}
	logCollectResults([]collect.ExportResult{result})
}

func printWechatChannelsPreview(w io.Writer, content *platform.Content) {
	if content == nil {
		return
	}
	fmt.Fprintln(w, "WeChat Channels import preview")
	fmt.Fprintf(w, "Title: %s\n", content.Title)
	if content.Author != nil && content.Author.Name != "" {
		fmt.Fprintf(w, "Author: %s\n", content.Author.Name)
	}
	if content.PublishedAt != "" {
		fmt.Fprintf(w, "Published: %s\n", content.PublishedAt)
	}
	if content.Text != "" {
		fmt.Fprintf(w, "Description: %s\n", content.Text)
	}
}

func confirmWechatChannelsImport(in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, "Import this video metadata locally? [y/N]: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func persistWechatChannelsImport(ctx context.Context, cfg *config.Config, sourceURL string, out map[string]any, result *collect.ExportResult) error {
	match := collect.Match{Platform: wechatchannels.PlatformName, Tool: "wechat.channels.import"}
	if cfg.Collect.UseIndexJSON() {
		entry := collect.EntryFromExport(sourceURL, match.Platform, out, result.Paths, result.Written)
		if err := collect.UpsertEntry(cfg.Collect.OutputDir, entry); err != nil {
			return err
		}
		result.Written = append(result.Written, collect.IndexPath(cfg.Collect.OutputDir))
	}
	if !cfg.Collect.UseSQLite() {
		return nil
	}
	db, err := collect.CollectDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	in := collect.UpsertInputFromExport(sourceURL, match, out, result.Paths, result.Downloaded, cfg.Collect.OutputDir)
	_, err = db.Upsert(ctx, in)
	return err
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
	_ = fs.Parse(args)

	profilePath := profilePathForMCP(*configPath)
	cfg, err := loadRequiredConfig(profilePath)
	if err != nil {
		log.Fatalf("load MCP profile: %v (run `digcrawl init --profile skill` or pass --config)", err)
	}
	allowDefaultSkillProfileTools(cfg)
	rt, err := runtime.NewRuntime(cfg)
	if err != nil {
		log.Fatalf("runtime: %v", err)
	}
	log.Printf("DigCrawl MCP server on stdio (instance=%s)", cfg.Gateway.InstanceID)
	if err := mcpgw.ServeStdio(rt); err != nil {
		log.Fatalf("mcp: %v", err)
	}
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
