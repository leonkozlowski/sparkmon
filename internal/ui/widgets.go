package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Spark is a multi-row column chart with per-cell threshold coloring.
// Each value renders as a vertical bar filled from the bottom; the bar height
// encodes value/maxValue, the color encodes the threshold band of that value.
type Spark struct {
	*tview.Box
	values []float64
	maxVal float64 // 0 = auto-scale to the max of visible values
	label  string
	// thresholds in the same units as maxVal; default = % thresholds (70/90 of max).
	warn float64
	crit float64
}

// NewSpark constructs a Spark with default % thresholds (70/90 of max).
func NewSpark() *Spark {
	return &Spark{Box: tview.NewBox()}
}

// SetLabel sets a one-line label rendered above the bars.
func (s *Spark) SetLabel(l string) *Spark { s.label = l; return s }

// SetMaxValue sets the value mapped to a full-height bar. Zero means auto-scale.
func (s *Spark) SetMaxValue(m float64) *Spark { s.maxVal = m; return s }

// SetThresholds sets the absolute (warn, crit) value cutoffs for coloring.
// Zero means "use 70% / 90% of the effective max."
func (s *Spark) SetThresholds(warn, crit float64) *Spark {
	s.warn, s.crit = warn, crit
	return s
}

// SetValues replaces all values.
func (s *Spark) SetValues(v []float64) *Spark { s.values = v; return s }

// AddValue appends a value and trims to keep at most max points.
func (s *Spark) AddValue(v float64, max int) *Spark {
	s.values = append(s.values, v)
	if len(s.values) > max {
		s.values = s.values[len(s.values)-max:]
	}
	return s
}

// Draw renders the Spark inside its inner rect.
func (s *Spark) Draw(screen tcell.Screen) {
	s.Box.DrawForSubclass(screen, s)
	x, y, w, h := s.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}

	plotTop := y
	plotH := h
	if s.label != "" && h > 1 {
		drawLabel(screen, x, y, w, s.label)
		plotTop = y + 1
		plotH = h - 1
	}
	if plotH <= 0 || len(s.values) == 0 {
		return
	}

	maxVal := s.maxVal
	if maxVal == 0 {
		for _, v := range s.values {
			if v > maxVal {
				maxVal = v
			}
		}
		if maxVal == 0 {
			maxVal = 1
		}
	}

	warn, crit := s.warn, s.crit
	if warn == 0 {
		warn = maxVal * 0.70
	}
	if crit == 0 {
		crit = maxVal * 0.90
	}

	levels := []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	start := 0
	if len(s.values) > w {
		start = len(s.values) - w
	}

	for i := start; i < len(s.values); i++ {
		col := x + (i - start)
		v := s.values[i]
		if v < 0 {
			v = 0
		}
		if v > maxVal {
			v = maxVal
		}
		units := int(v / maxVal * float64(plotH) * 8)
		fullCells := units / 8
		partial := units % 8

		color := thresholdColor(v, warn, crit)
		style := tcell.StyleDefault.Foreground(color)

		for r := 0; r < fullCells && r < plotH; r++ {
			screen.SetContent(col, plotTop+plotH-1-r, '█', nil, style)
		}
		if fullCells < plotH && partial > 0 {
			screen.SetContent(col, plotTop+plotH-1-fullCells, levels[partial], nil, style)
		}
	}
}

// CoreGrid lays out one tiny Spark per core in a grid. Per-core history lives
// inside each Spark.
type CoreGrid struct {
	*tview.Box
	cores []*Spark
}

// NewCoreGrid constructs an empty CoreGrid.
func NewCoreGrid() *CoreGrid {
	return &CoreGrid{Box: tview.NewBox()}
}

// Update appends a fresh per-core sample. Each entry becomes one bar in its
// core's sparkline history.
func (g *CoreGrid) Update(values []float64, hist int) {
	for len(g.cores) < len(values) {
		s := NewSpark().SetMaxValue(100)
		g.cores = append(g.cores, s)
	}
	for i, v := range values {
		g.cores[i].AddValue(v, hist)
	}
}

// Reset clears all per-core histories.
func (g *CoreGrid) Reset() {
	for _, s := range g.cores {
		s.values = nil
	}
}

// Draw arranges the per-core Sparks in a compact grid inside the box.
func (g *CoreGrid) Draw(screen tcell.Screen) {
	g.Box.DrawForSubclass(screen, g)
	x, y, w, h := g.GetInnerRect()
	n := len(g.cores)
	if n == 0 || w <= 0 || h <= 0 {
		return
	}

	cols := chooseGridCols(n, w)
	rows := (n + cols - 1) / cols
	cellW := w / cols
	cellH := h / rows
	if cellW < 1 || cellH < 1 {
		return
	}

	for i, s := range g.cores {
		if i >= cols*rows {
			break
		}
		col := i % cols
		row := i / cols
		s.SetRect(x+col*cellW, y+row*cellH, cellW-1, cellH) // -1 for a thin gutter
		s.Draw(screen)
	}
}

// chooseGridCols picks a column count that yields roughly square cells while
// keeping each cell at least 4 chars wide.
func chooseGridCols(n, w int) int {
	if n <= 1 {
		return 1
	}
	const minCellW = 4
	maxCols := w / minCellW
	if maxCols < 1 {
		maxCols = 1
	}
	if maxCols >= n {
		return n
	}
	cols := 1
	for cols*cols < n && cols < maxCols {
		cols++
	}
	if cols > maxCols {
		cols = maxCols
	}
	return cols
}

// ---- shared helpers -----------------------------------------------------------

func drawLabel(screen tcell.Screen, x, y, w int, label string) {
	style := tcell.StyleDefault.Foreground(tcell.ColorGray)
	col := x
	for _, r := range label {
		if col >= x+w {
			return
		}
		screen.SetContent(col, y, r, nil, style)
		col++
	}
}

func thresholdColor(v, warn, crit float64) tcell.Color {
	switch {
	case v >= crit:
		return tcell.ColorRed
	case v >= warn:
		return tcell.ColorYellow
	default:
		return tcell.ColorGreen
	}
}
