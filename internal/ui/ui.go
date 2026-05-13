// Package ui renders the sparkmon dashboard with jig (tview under the hood).
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atterpac/jig/components"
	"github.com/atterpac/jig/layout"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/leonkozlowski/sparkmon/internal/config"
	"github.com/leonkozlowski/sparkmon/internal/metrics"
)

// nodeWidgets are the live widgets for one node column.
type nodeWidgets struct {
	panel  *components.Panel
	status *tview.TextView
	cpu    *components.Sparkline
	mem    *components.ProgressBar
	gpu    *tview.TextView
	netRx  *components.Sparkline
	netTx  *components.Sparkline
}

// UI owns the jig application and the per-node widgets.
type UI struct {
	app     *layout.App
	cfg     *config.Config
	top     *tview.TextView
	nodes   []*nodeWidgets
	hist    int
	onForce func()
}

// New builds the dashboard for the given nodes (it does not start it).
func New(cfg *config.Config, nodes []config.Node) *UI {
	u := &UI{cfg: cfg, hist: cfg.HistoryLen()}

	u.top = tview.NewTextView().SetDynamicColors(true)

	cluster := tview.NewFlex().SetDirection(tview.FlexColumn)
	for _, n := range nodes {
		w := newNodeWidgets(n.Display())
		u.nodes = append(u.nodes, w)
		cluster.AddItem(w.panel, 0, 1, false)
	}

	root := components.NewComponentBase(cluster).
		SetName("cluster").
		SetHints([]components.KeyHint{
			{Key: "r", Description: "refresh now"},
			{Key: "t", Description: "theme"},
			{Key: "q", Description: "quit"},
		})

	u.app = layout.NewApp(layout.AppConfig{
		TopBar:       u.top,
		TopBarHeight: 1,
	})
	u.app.Pages().Push(root)
	u.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlC {
			u.app.Stop()
			return nil
		}
		switch ev.Rune() {
		case 'q', 'Q':
			u.app.Stop()
			return nil
		case 'r', 'R':
			if u.onForce != nil {
				u.onForce()
			}
			return nil
		}
		return ev
	})
	return u
}

func newNodeWidgets(title string) *nodeWidgets {
	w := &nodeWidgets{
		status: tview.NewTextView().SetDynamicColors(true),
		cpu:    components.NewSparkline().SetLabel("CPU %").SetMaxValue(100),
		mem:    components.NewProgressBar().SetLabel("MEM").SetShowPercentage(true),
		gpu:    tview.NewTextView().SetDynamicColors(true),
		netRx:  components.NewSparkline().SetLabel("net rx"),
		netTx:  components.NewSparkline().SetLabel("net tx"),
	}

	net := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(w.netRx, 0, 1, false).
		AddItem(w.netTx, 0, 1, false)

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.status, 3, 0, false).
		AddItem(w.cpu, 0, 3, false).
		AddItem(w.mem, 1, 0, false).
		AddItem(label(" GPU"), 1, 0, false).
		AddItem(w.gpu, 0, 2, false).
		AddItem(net, 0, 2, false)

	w.panel = components.NewPanel().SetTitle(" " + title + " ")
	w.panel.SetContent(body)
	return w
}

func label(s string) *tview.TextView {
	return tview.NewTextView().SetDynamicColors(true).SetText("[::b]" + s)
}

// SetOnForceRefresh registers a callback invoked when the user presses "r".
func (u *UI) SetOnForceRefresh(fn func()) { u.onForce = fn }

// Run starts the event loop and blocks until the app stops.
func (u *UI) Run() error { return u.app.Run() }

// Stop tears down the app.
func (u *UI) Stop() { u.app.Stop() }

// Submit applies a fresh batch of snapshots; it is safe to call from any goroutine.
func (u *UI) Submit(snaps []metrics.Snapshot) {
	u.app.QueueUpdateDraw(func() { u.render(snaps) })
}

func (u *UI) render(snaps []metrics.Snapshot) {
	upNodes, upGPUs := 0, 0
	for _, s := range snaps {
		if s.Up {
			upNodes++
		}
		upGPUs += s.UpGPUs()
	}
	u.top.SetText(fmt.Sprintf(
		" [::b]sparkmon[::-]  •  %d/%d nodes  •  %d GPU(s)  •  %s  •  every %s  •  [yellow]r[-]efresh [yellow]t[-]heme [yellow]q[-]uit",
		upNodes, len(snaps), upGPUs, time.Now().Format("15:04:05"), u.cfg.PollEvery()))

	for i := range u.nodes {
		w := u.nodes[i]
		if i >= len(snaps) {
			continue
		}
		s := snaps[i]

		if !s.Up {
			w.status.SetText(fmt.Sprintf("[red]● DOWN[-]\n%s\n", trunc(orDash(s.Err), 80)))
			w.cpu.AddValue(0, u.hist)
			w.mem.SetProgress(0)
			w.netRx.AddValue(0, u.hist)
			w.netTx.AddValue(0, u.hist)
			w.gpu.SetText("[gray]—")
			continue
		}

		w.status.SetText(fmt.Sprintf(
			"[green]● up[-]   uptime %s\nCPU %3.0f%%  ·  %d cores  ·  load %.2f  ·  mem %s/%s (%.0f%%)  ·  / %.0f%%\ndisk r/w %s · %s    net rx/tx %s · %s",
			fmtDur(s.Uptime),
			s.CPUPct, s.NCPU, s.Load1, human(s.MemUsedB), human(s.MemTotalB), s.MemUsedPct, s.RootFSPct,
			humanRate(s.DiskRBps), humanRate(s.DiskWBps), humanRate(s.NetRxBps), humanRate(s.NetTxBps)))

		w.cpu.AddValue(s.CPUPct, u.hist)
		w.mem.SetProgress(clamp01(s.MemUsedPct / 100))
		w.netRx.AddValue(s.NetRxBps, u.hist)
		w.netTx.AddValue(s.NetTxBps, u.hist)

		switch {
		case !s.GPUUp:
			w.gpu.SetText(fmt.Sprintf("[gray]dcgm-exporter unreachable: %s", trunc(orDash(s.GPUErr), 70)))
		case len(s.GPUs) == 0:
			w.gpu.SetText("[gray]no GPUs reported")
		default:
			var b strings.Builder
			for _, g := range s.GPUs {
				model := g.Model
				if model == "" {
					model = "GPU"
				}
				fmt.Fprintf(&b, "[::b]%s[::-] %s\n  util %3.0f%%   vram %s/%s   %.0f°C   %.0f W   %.0f MHz\n",
					"gpu"+g.Index, model, g.UtilPct, human(g.MemUsedB), human(g.MemTotalB), g.TempC, g.PowerW, g.SMClockMHz)
			}
			w.gpu.SetText(strings.TrimRight(b.String(), "\n"))
		}
	}
}

// ---- small formatting helpers -------------------------------------------------

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no detail)"
	}
	return s
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func fmtDur(d time.Duration) string {
	if d <= 0 {
		return "?"
	}
	d = d.Round(time.Minute)
	day := d / (24 * time.Hour)
	d -= day * 24 * time.Hour
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	switch {
	case day > 0:
		return fmt.Sprintf("%dd %dh", day, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func human(v float64) string {
	if v < 0 || v != v { // negative or NaN
		return "—"
	}
	const k = 1024.0
	if v < k {
		return fmt.Sprintf("%.0f B", v)
	}
	v /= k
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB"} {
		if v < k {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
		v /= k
	}
	return fmt.Sprintf("%.1f PiB", v)
}

func humanRate(v float64) string {
	if v <= 0 || v != v {
		return "0 B/s"
	}
	return human(v) + "/s"
}
