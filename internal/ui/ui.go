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

// DGX Spark (GB10) operating envelope. The GB10 superchip shares a single
// ~128 GB LPDDR5X pool between the Grace CPU and Blackwell GPU over NVLink-C2C,
// so "system memory" and "VRAM" are the same physical pool — we show one
// unified figure rather than two competing graphs.
const (
	tempWarnC  = 80.0  // GPU junction warming
	tempCritC  = 87.0  // throttle territory on a compact chassis
	powerWarnW = 110.0 // approaching the ~140 W envelope
	powerCritW = 135.0
	powerMaxW  = 150.0 // sparkline full-scale
	memWarnPct = 80.0
	memCritPct = 92.0
	// throttleClockFrac: a GPU pinned busy whose SM clock has fallen well below
	// the peak we've observed is almost certainly therm/power throttling.
	throttleClockFrac = 0.85
	throttleUtilPct   = 80.0
)

// nodeWidgets are the live widgets for one node column.
type nodeWidgets struct {
	panel     *components.Panel
	body      *tview.Flex
	status    *tview.TextView
	health    *components.ProgressBar
	cardUtil  *components.MetricCard
	cardTemp  *components.MetricCard
	cardPower *components.MetricCard
	cardMem   *components.MetricCard
	gpuLine   *tview.TextView // per-GPU detail (clock is the throttle tell)
	inferLine *tview.TextView // vLLM serving stats (hidden when no vllm_port)
	tokGraph  *components.LineGraph
	cores     *CoreGrid // per-core CPU activity
	net       *components.LineGraph
	disk      *components.LineGraph

	// rolling state for trend arrows and throttle detection
	maxClock  map[string]float64
	prevUtil  float64
	prevTemp  float64
	prevPower float64
	prevMem   float64
	havePrev  bool
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
		health:    components.NewProgressBar().SetShowPercentage(true),
		cardUtil:  kpiCard("GPU", "%", 100),
		cardTemp:  kpiCard("Temp", "°C", 100).SetThresholds(tempWarnC, tempCritC, true),
		cardPower: kpiCard("Power", "W", powerMaxW).SetThresholds(powerWarnW, powerCritW, true),
		cardMem:   kpiCard("Mem", "%", 100).SetThresholds(memWarnPct, memCritPct, true),
		gpuLine:   tview.NewTextView().SetDynamicColors(true),
		inferLine: tview.NewTextView().SetDynamicColors(true),
		tokGraph:  components.NewLineGraph().SetTitle("tok/s").SetAutoScale(true),
		cores:     NewCoreGrid(),
		net:       components.NewLineGraph().SetTitle("net B/s").SetAutoScale(true),
		disk:      components.NewLineGraph().SetTitle("disk B/s").SetAutoScale(true),
		maxClock:  map[string]float64{},
	}
	// The autoscaled Y labels on byte/throughput graphs render as noise; the
	// title plus the trace shape carry the signal, so hide the axis numbers.
	noisyAxis := components.AxisConfig{Show: false}
	w.net.SetYAxis(noisyAxis)
	w.disk.SetYAxis(noisyAxis)
	w.tokGraph.SetYAxis(noisyAxis)

	// 2×2 KPI grid: the four signals that actually predict a Spark falling over.
	cardsTop := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(w.cardUtil, 0, 1, false).
		AddItem(w.cardTemp, 0, 1, false)
	cardsBot := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(w.cardPower, 0, 1, false).
		AddItem(w.cardMem, 0, 1, false)
	cards := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(cardsTop, 0, 1, false).
		AddItem(cardsBot, 0, 1, false)

	// Cards draw from the top in ~7 rows each; give the 2×2 grid a fixed height
	// so the panel doesn't leave a big empty band under them.
	w.body = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.status, 1, 0, false).
		AddItem(w.health, 1, 0, false).
		AddItem(cards, 14, 0, false).
		AddItem(w.gpuLine, 1, 0, false).
		AddItem(w.inferLine, 1, 0, false).
		AddItem(w.tokGraph, 0, 2, false).
		AddItem(withLabel("cores", w.cores), 0, 2, false).
		AddItem(w.net, 0, 2, false).
		AddItem(w.disk, 0, 2, false)

	w.panel = components.NewPanel().SetTitle(" " + title + " ")
	w.panel.SetContent(w.body)
	return w
}

// kpiCard builds a metric card with a fixed-scale sparkline.
func kpiCard(label, unit string, sparkMax float64) *components.MetricCard {
	return components.NewMetricCard().
		SetLabel(label).
		SetUnit(unit).
		SetCompact(false).
		SetShowBorder(true).
		SetSparklineMax(sparkMax)
}

// withLabel stacks a dim one-line caption above a primitive.
func withLabel(label string, p tview.Primitive) *tview.Flex {
	cap := tview.NewTextView().SetDynamicColors(true).SetText("[gray]" + label)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(cap, 1, 0, false).
		AddItem(p, 0, 1, false)
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
	var clusterPower, maxTemp float64
	for _, s := range snaps {
		if s.Up {
			upNodes++
		}
		upGPUs += s.UpGPUs()
		for _, g := range s.GPUs {
			clusterPower += g.PowerW
			if g.TempC > maxTemp {
				maxTemp = g.TempC
			}
		}
	}
	u.top.SetText(fmt.Sprintf(
		" [::b]sparkmon[::-]  •  %d/%d nodes  •  %d GPU(s)  •  %.0f W  •  %s peak  •  %s  •  every %s",
		upNodes, len(snaps), upGPUs, clusterPower, tempTag(maxTemp),
		time.Now().Format("15:04:05"), u.cfg.PollEvery()))

	for i := range u.nodes {
		if i >= len(snaps) {
			continue
		}
		u.renderNode(u.nodes[i], snaps[i])
	}
}

func (u *UI) renderNode(w *nodeWidgets, s metrics.Snapshot) {
	if !s.Up {
		w.markDown(s)
		return
	}

	// Aggregate the GPU(s) into the headline figures the cards show.
	var maxUtil, maxTemp, sumPower float64
	throttling := false
	for _, g := range s.GPUs {
		if g.UtilPct > maxUtil {
			maxUtil = g.UtilPct
		}
		if g.TempC > maxTemp {
			maxTemp = g.TempC
		}
		sumPower += g.PowerW
		if g.SMClockMHz > w.maxClock[g.Index] {
			w.maxClock[g.Index] = g.SMClockMHz
		}
		if g.UtilPct >= throttleUtilPct && g.SMClockMHz > 0 &&
			g.SMClockMHz < throttleClockFrac*w.maxClock[g.Index] {
			throttling = true
		}
	}

	health := healthScore(s, maxTemp, throttling)

	// Status line: model · uptime · freshness, plus any partial-failure badges.
	model := "node"
	if len(s.GPUs) > 0 {
		model = shortModel(s.GPUs[0].Model)
	}
	status := fmt.Sprintf("[white]%s[-]  [gray]up %s · %s ago", model,
		humanUptime(s.Uptime), humanDur(time.Since(s.When)))
	if throttling {
		status += "  [black:red] THROTTLING [-:-]"
	}
	if !s.GPUUp {
		status += "  [yellow]gpu: " + firstLine(s.GPUErr)
	}
	w.status.SetText(status)

	w.health.SetProgress(clamp01(health / 100))
	w.panel.SetTitleColor(healthColor(health))

	// KPI cards. Util is "busy" not "bad", so its trend reads green; temp/power/
	// mem rising is bad, so theirs reads red.
	memPct := s.MemUsedPct
	setCard(w.cardUtil, maxUtil, w.prevUtil, w.havePrev, u.hist, true)
	setCard(w.cardTemp, maxTemp, w.prevTemp, w.havePrev, u.hist, false)
	setCard(w.cardPower, sumPower, w.prevPower, w.havePrev, u.hist, false)
	setCard(w.cardMem, memPct, w.prevMem, w.havePrev, u.hist, false)
	w.prevUtil, w.prevTemp, w.prevPower, w.prevMem = maxUtil, maxTemp, sumPower, memPct
	w.havePrev = true

	w.renderGPULine(s)
	w.renderInfer(s, u.hist)

	if len(s.PerCoreUtil) > 0 {
		w.cores.Update(s.PerCoreUtil, u.hist)
	}
	w.net.AddValue(s.NetRxBps+s.NetTxBps, u.hist)
	w.disk.AddValue(s.DiskRBps+s.DiskWBps, u.hist)
}

// renderGPULine shows one row per GPU. The SM clock is the value that exposes
// throttling, which none of the cards surface on their own.
func (w *nodeWidgets) renderGPULine(s metrics.Snapshot) {
	if len(s.GPUs) == 0 {
		w.gpuLine.SetText("[gray]no GPUs reported")
		w.body.ResizeItem(w.gpuLine, 1, 0)
		return
	}
	// No framebuffer figure: on GB10's unified memory dcgm reports 0 MiB of
	// dedicated VRAM, so the Mem card (system memory) is the real pool. Clock is
	// kept because it's the throttle tell.
	var b strings.Builder
	for i, g := range s.GPUs {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "gpu%s %s%4.0f%% util[-] · %s · [gray]%.0f MHz · %.0f W",
			g.Index, utilTag(g.UtilPct), g.UtilPct,
			tempTag(g.TempC), g.SMClockMHz, g.PowerW)
	}
	w.gpuLine.SetText(b.String())
	w.body.ResizeItem(w.gpuLine, len(s.GPUs), 0)
}

// renderInfer shows vLLM serving health and toggles the workload widgets. When
// no vllm_port is configured the line and tok/s graph collapse to zero height.
func (w *nodeWidgets) renderInfer(s metrics.Snapshot, hist int) {
	if !s.InferOn {
		w.body.ResizeItem(w.inferLine, 0, 0)
		w.body.ResizeItem(w.tokGraph, 0, 0)
		return
	}
	w.body.ResizeItem(w.inferLine, 1, 0)
	w.body.ResizeItem(w.tokGraph, 0, 2)

	if !s.InferUp {
		w.inferLine.SetText("[yellow]vLLM: " + firstLine(s.InferErr))
		w.tokGraph.Clear()
		return
	}

	model := s.InferModel
	if model == "" {
		model = "vLLM"
	}
	waitTag := "[gray]"
	if s.ReqWaiting > 0 {
		waitTag = "[yellow]" // queue building = backpressure
	}
	w.inferLine.SetText(fmt.Sprintf(
		"[white]%s[-]  [green]%.0f run[-] · %s%.0f wait[-] · KV %s%.0f%%[-] · [::b]%s tok/s",
		shortModel(model), s.ReqRunning, waitTag, s.ReqWaiting,
		colorTag(s.KVCachePct, 80, 95), s.KVCachePct, humanCount(s.GenTokPerSec)))
	w.tokGraph.AddValue(s.GenTokPerSec, hist)
}

// markDown blanks a node's widgets and shows why it's unreachable.
func (w *nodeWidgets) markDown(s metrics.Snapshot) {
	w.status.SetText("[red::b]DOWN[-:-]  [gray]" + firstLine(s.Err))
	w.health.SetProgress(0)
	w.panel.SetTitleColor(tcell.ColorRed)
	for _, c := range []*components.MetricCard{w.cardUtil, w.cardTemp, w.cardPower, w.cardMem} {
		c.SetValue("—").SetSparkline(nil).SetTrend(components.TrendNeutral, "", true)
	}
	w.gpuLine.SetText("")
	w.inferLine.SetText("")
	w.tokGraph.Clear()
	w.cores.Reset()
	w.net.Clear()
	w.disk.Clear()
	w.havePrev = false
}

// healthScore derives a 0–100 health figure, folding in the Spark-specific
// thermal and throttle signals on top of host pressure.
func healthScore(s metrics.Snapshot, maxTemp float64, throttling bool) float64 {
	health := 100.0
	switch {
	case s.CPUPct > 90:
		health -= 20
	case s.CPUPct > 75:
		health -= 10
	}
	switch {
	case s.MemUsedPct > memCritPct:
		health -= 30
	case s.MemUsedPct > memWarnPct:
		health -= 15
	}
	switch {
	case s.RootFSPct > 90:
		health -= 20
	case s.RootFSPct > 80:
		health -= 10
	}
	switch {
	case s.Load1 > float64(s.NCPU)*2.0:
		health -= 20
	case s.Load1 > float64(s.NCPU):
		health -= 10
	}
	switch {
	case maxTemp >= tempCritC:
		health -= 25
	case maxTemp >= tempWarnC:
		health -= 10
	}
	if throttling {
		health -= 20
	}
	if health < 0 {
		health = 0
	}
	return health
}

// setCard updates one KPI card's value, sparkline point, and trend arrow.
// goodWhenUp flags metrics where a rising value is benign (e.g. utilization).
func setCard(c *components.MetricCard, cur, prev float64, havePrev bool, hist int, goodWhenUp bool) {
	c.SetValue(fmt.Sprintf("%.0f", cur))
	c.AddSparkValue(cur, hist)
	tr, delta := components.TrendNeutral, ""
	if havePrev {
		switch d := cur - prev; {
		case d >= 1:
			tr, delta = components.TrendUp, fmt.Sprintf("+%.0f", d)
		case d <= -1:
			tr, delta = components.TrendDown, fmt.Sprintf("%.0f", d)
		}
	}
	c.SetTrend(tr, delta, goodWhenUp)
}

// ---- formatting helpers -------------------------------------------------------

func shortModel(m string) string {
	m = strings.TrimSpace(strings.TrimPrefix(m, "NVIDIA "))
	if m == "" {
		return "GPU"
	}
	return m
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "unreachable"
	}
	return s
}

func humanUptime(d time.Duration) string {
	if d <= 0 {
		return "?"
	}
	days := int(d.Hours()) / 24
	hrs := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hrs)
	}
	if hrs > 0 {
		return fmt.Sprintf("%dh%dm", hrs, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// humanCount renders a rate compactly: 942, 1.2k, 18k.
func humanCount(v float64) string {
	switch {
	case v >= 10000:
		return fmt.Sprintf("%.0fk", v/1000)
	case v >= 1000:
		return fmt.Sprintf("%.1fk", v/1000)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

func humanDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.0fs", d.Seconds())
}

func utilTag(p float64) string    { return colorTag(p, 70, 90) }
func tempTagPct(p float64) string { return colorTag(p, tempWarnC, tempCritC) }

// tempTag renders a temperature with a threshold-colored value and °C suffix.
func tempTag(t float64) string {
	if t <= 0 {
		return "[gray]–°C"
	}
	return fmt.Sprintf("%s%.0f°C[-]", tempTagPct(t), t)
}

// colorTag returns an opening color tag for v against warn/crit cutoffs.
func colorTag(v, warn, crit float64) string {
	switch {
	case v >= crit:
		return "[red]"
	case v >= warn:
		return "[yellow]"
	default:
		return "[green]"
	}
}

func healthColor(h float64) tcell.Color {
	switch {
	case h < 50:
		return tcell.ColorRed
	case h < 80:
		return tcell.ColorYellow
	default:
		return tcell.ColorGreen
	}
}

// clamp01 clamps a value between 0 and 1.
func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
