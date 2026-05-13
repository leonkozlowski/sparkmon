// Command sparkmon is a terminal dashboard for a small NVIDIA DGX Spark cluster.
// It polls node_exporter (:9100) and dcgm-exporter (:9400) on each configured
// node and renders a unified, side-by-side view.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/atterpac/jig/theme"
	"github.com/atterpac/jig/theme/themes"
	"github.com/leonkozlowski/sparkmon/internal/config"
	"github.com/leonkozlowski/sparkmon/internal/metrics"
	"github.com/leonkozlowski/sparkmon/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sparkmon:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	nodesFlag := flag.String("nodes", "", "comma-separated name=host pairs (overrides config nodes), e.g. spark-01=10.0.0.1,spark-02=10.0.0.2")
	intervalFlag := flag.Duration("interval", 0, "poll interval (overrides config)")
	themeNameFlag := flag.String("theme", "", "theme name (overrides config file)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		if *nodesFlag == "" {
			return fmt.Errorf("%v\n(provide -config <file> or -nodes name=host,name=host)", err)
		}
		cfg = config.Default()
	}
	if *nodesFlag != "" {
		ns, err := config.ParseNodesFlag(*nodesFlag)
		if err != nil {
			return fmt.Errorf("-nodes: %w", err)
		}
		cfg.Nodes = ns
	}
	if *intervalFlag > 0 {
		cfg.Interval = intervalFlag.String()
	}
	if len(cfg.Nodes) == 0 {
		return fmt.Errorf("no nodes configured")
	}

	// Initialize theme system
	themeName := cfg.Theme
	if *themeNameFlag != "" {
		themeName = *themeNameFlag
	}
	selectedTheme := themes.Get(themeName)
	if selectedTheme == nil {
		fmt.Fprintf(os.Stderr, "Warning: theme %q not found, using default\n", themeName)
		selectedTheme = themes.Default()
	}
	theme.SetProvider(selectedTheme)

	every := cfg.PollEvery()
	coll := metrics.NewCollector(cfg.Nodes, clampDur(every, time.Second, 5*time.Second))
	app := ui.New(cfg, cfg.Nodes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	force := make(chan struct{}, 1)
	app.SetOnForceRefresh(func() {
		select {
		case force <- struct{}{}:
		default:
		}
	})

	go func() {
		poll := func() {
			cctx, c := context.WithTimeout(ctx, every)
			snaps := coll.Collect(cctx)
			c()
			app.Submit(snaps)
		}
		poll()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				poll()
			case <-force:
				poll()
			}
		}
	}()

	return app.Run()
}

func clampDur(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}
