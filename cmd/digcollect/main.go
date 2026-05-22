package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/originaleric/digcrawl/collect"
	"github.com/originaleric/digcrawl/config"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "inbox" {
		runInbox(os.Args[2:])
		return
	}
	runCollect(os.Args[1:])
}

func runCollect(args []string) {
	fs := flag.NewFlagSet("digcollect", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	outDir := fs.String("out", "", "output directory")
	format := fs.String("format", "", "export formats: md,html,json")
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
		logResults(results)
		if err != nil {
			log.Fatalf("collect file: %v", err)
		}
		return
	}
	if len(fs.Args()) == 0 {
		log.Fatal("usage: digcollect <url> [--file urls.txt] [--format md,json] [--out dir]")
	}
	result, err := svc.CollectURL(ctx, fs.Args()[0], formats, cfg.Collect.OutputDir)
	if err != nil {
		log.Fatalf("collect: %v", err)
	}
	logResults([]collect.ExportResult{result})
}

func runInbox(args []string) {
	fs := flag.NewFlagSet("digcollect inbox", flag.ExitOnError)
	configPath := fs.String("config", "config/config.yaml", "path to config.yaml")
	outDir := fs.String("out", "", "output directory")
	format := fs.String("format", "", "export formats: md,html,json")
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
		if err := svc.WatchInbox(ctx, formats, cfg.Collect.OutputDir); err != nil && err != context.Canceled {
			log.Fatalf("inbox watch: %v", err)
		}
		return
	}
	results, err := svc.ProcessInbox(ctx, formats, cfg.Collect.OutputDir)
	logResults(results)
	if err != nil {
		log.Fatalf("inbox: %v", err)
	}
}

func loadConfig(path string) (*config.Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return config.Get(), nil
		}
		return nil, err
	}
	return config.Load(path)
}

func parseFormats(raw string, defaults []string) []string {
	if strings.TrimSpace(raw) == "" {
		return defaults
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func logResults(results []collect.ExportResult) {
	for _, r := range results {
		for _, w := range r.Written {
			log.Printf("wrote %s", w)
		}
	}
}
