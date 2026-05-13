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

// gpuWidgets are the live widgets for one GPU within a node column.
type gpuWidgets struct {
	row    *tview.Flex
	header *tview.TextView
	util   *Spark
}

// nodeWidgets are the live widgets for one node column.
type nodeWidgets struct {
	panel     *components.Panel
	status    *tview.TextView
	cpu       *Spark
	cores     *CoreGrid
	mem       *components.ProgressBar
	gpuStatus *tview.TextView
	gpuFlex   *tview.Flex
	gpus      map[string]*gpuWidgets
	gpuOrder  []string
	netRx     *Spark
	netTx     *Spark
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
		status:    tview.NewTextView().SetDynamicColors(true),
		cpu:       NewSpark().SetLabel("CPU %").SetMaxValue(100),
		cores:     NewCoreGrid(),
		mem:       components.NewProgressBar().SetLabel("MEM").SetShowPercentage(true),
		gpuStatus: tview.NewTextView().SetDynamicColors(true),
		gpuFlex:   tview.NewFlex().SetDirection(tview.FlexRow),
		gpus:      map[string]*gpuWidgets{},
		netRx:     NewSpark().SetLabel("net rx B/s"),
		netTx:     NewSpark().SetLabel("net tx B/s"),
	}

	net := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(w.netRx, 0, 1, false).
		AddItem(w.netTx, 0, 1, false)

	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.status, 3, 0, false).
		AddItem(w.cpu, 0, 3, false).
		AddItem(label(" cores"), 1, 0, false).
		AddItem(w.cores, 0, 4, false).
		AddItem(w.mem, 1, 0, false).
		AddItem(label(" GPU"), 1, 0, false).
		AddItem(w.gpuStatus, 1, 0, false).
		AddItem(w.gpuFlex, 0, 5, false).
		AddItem(net, 0, 3, false)

	w.panel = components.NewPanel().SetTitle(" " + title + " ")
	w.panel.SetContent(body)
	return w
}

func newGPUWidgets() *gpuWidgets {
	g := &gpuWidgets{
		header: tview.NewTextView().SetDynamicColors(true),
		util:   NewSpark().SetLabel("util %").SetMaxValue(100),
	}
	g.row = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(g.header, 1, 0, false).
		AddItem(g.util, 0, 4, false)
	return g
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
		u.renderNode(w, snaps[i])
	}
}

func (u *UI) renderNode(w *nodeWidgets, s metrics.Snapshot) {
	if !s.Up {
		w.status.SetText(fmt.Sprintf("[red]● DOWN[-]\n%s\n", trunc(orDash(s.Err), 80)))
		w.cpu.AddValue(0, u.hist)
		w.cores.Reset()
		w.mem.SetProgress(0)
		w.netRx.AddValue(0, u.hist)
		w.netTx.AddValue(0, u.hist)
		w.gpuStatus.SetText("[gray]—")
		u.clearGPUs(w)
		return
	}

	cpuCol := colorByPct(s.CPUPct)
	memCol := colorByPct(s.MemUsedPct)
	rootCol := colorByPct(s.RootFSPct)
	loadCol := colorByLoad(s.Load1, s.NCPU)

	w.status.SetText(fmt.Sprintf(
		"[green]● up[-]   uptime %s\n"+
			"CPU [%s]%3.0f%%[-]  ·  %d cores  ·  load [%s]%.2f[-]  ·  mem [%s]%s/%s (%.0f%%)[-]  ·  / [%s]%.0f%%[-]\n"+
			"disk r/w %s · %s    net rx/tx %s · %s",
		fmtDur(s.Uptime),
		cpuCol, s.CPUPct, s.NCPU, loadCol, s.Load1,
		memCol, human(s.MemUsedB), human(s.MemTotalB), s.MemUsedPct,
		rootCol, s.RootFSPct,
		humanRate(s.DiskRBps), humanRate(s.DiskWBps), humanRate(s.NetRxBps), humanRate(s.NetTxBps)))

	w.cpu.AddValue(s.CPUPct, u.hist)
	w.cores.Update(s.PerCoreUtil, u.hist)
	w.mem.SetProgress(clamp01(s.MemUsedPct / 100))
	w.netRx.AddValue(s.NetRxBps, u.hist)
	w.netTx.AddValue(s.NetTxBps, u.hist)

	u.renderGPUs(w, s)
}

func (u *UI) renderGPUs(w *nodeWidgets, s metrics.Snapshot) {
	switch {
	case !s.GPUUp:
		w.gpuStatus.SetText(fmt.Sprintf("[red]dcgm-exporter unreachable: %s", trunc(orDash(s.GPUErr), 70)))
		u.clearGPUs(w)
		return
	case len(s.GPUs) == 0:
		w.gpuStatus.SetText("[gray]no GPUs reported")
		u.clearGPUs(w)
		return
	}
	w.gpuStatus.SetText("")

	for _, g := range s.GPUs {
		gw, ok := w.gpus[g.Index]
		if !ok {
			gw = newGPUWidgets()
			w.gpus[g.Index] = gw
			w.gpuOrder = append(w.gpuOrder, g.Index)
			w.gpuFlex.AddItem(gw.row, 0, 1, false)
		}
		model := g.Model
		if model == "" {
			model = "GPU"
		}
		vram := ""
		if g.MemTotalB > 0 {
			pct := clampf(100*g.MemUsedB/g.MemTotalB, 0, 100)
			vram = fmt.Sprintf(" · vram %s/%s ([%s]%.0f%%[-])",
				human(g.MemUsedB), human(g.MemTotalB), colorByPct(pct), pct)
		}
		gw.header.SetText(fmt.Sprintf(
			"[::b]gpu%s[::-] %s · util [%s]%3.0f%%[-] · [%s]%.0f°C[-] · %.0f W · %.0f MHz%s",
			g.Index, model,
			colorByPct(g.UtilPct), g.UtilPct,
			colorByTemp(g.TempC), g.TempC,
			g.PowerW, g.SMClockMHz,
			vram))
		gw.util.AddValue(g.UtilPct, u.hist)
	}
}

// clearGPUs zeros existing GPU widgets without tearing them down (preserves
// history if the exporter blips and then comes back).
func (u *UI) clearGPUs(w *nodeWidgets) {
	for _, idx := range w.gpuOrder {
		gw := w.gpus[idx]
		gw.header.SetText("")
		gw.util.AddValue(0, u.hist)
	}
}

// ---- threshold coloring -------------------------------------------------------

// colorByPct returns a tview color tag (no brackets) for a 0-100 percentage.
func colorByPct(v float64) string {
	switch {
	case v >= 90:
		return "red"
	case v >= 70:
		return "yellow"
	default:
		return "green"
	}
}

// colorByTemp returns a color tag for a temperature in °C.
func colorByTemp(t float64) string {
	switch {
	case t >= 85:
		return "red"
	case t >= 75:
		return "yellow"
	default:
		return "green"
	}
}

// colorByLoad returns a color tag for a 1-min load average given the core count.
func colorByLoad(load float64, ncores int) string {
	if ncores <= 0 {
		return "white"
	}
	r := load / float64(ncores)
	switch {
	case r >= 1.5:
		return "red"
	case r >= 1.0:
		return "yellow"
	default:
		return "green"
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

func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
