package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colBorder     = lipgloss.Color("240")
	colFocus      = lipgloss.Color("#61afef")
	colDim        = lipgloss.Color("244")
	colSelFocus   = lipgloss.Color("#264f78")
	colSelBlur    = lipgloss.Color("237")
	colBar        = lipgloss.Color("236")
	colKeyLabel   = lipgloss.Color("#3a6b8c")
	statusPalette = map[string]lipgloss.Color{
		"active":  "#61afef",
		"working": "#e5c07b",
		"waiting": "#c678dd",
		"idle":    "#98c379",
		"ended":   "240",
		"running": "#e5c07b",
		"done":    "#98c379",
	}

	stDim  = lipgloss.NewStyle().Foreground(colDim)
	stBold = lipgloss.NewStyle().Bold(true)
	stErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75"))
	stOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("#98c379"))
	stWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b"))
)

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return r.w > 0 && x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// box draws a rounded panel with a title (and optional right-aligned label)
// in the top border. Body lines are truncated/padded to the inner width.
func box(title, right string, body []string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	bc := colBorder
	ts := stDim
	if focused {
		bc, ts = colFocus, lipgloss.NewStyle().Foreground(colFocus).Bold(true)
	}
	b := lipgloss.NewStyle().Foreground(bc)
	inner := w - 2

	right = ansi.Truncate(right, max(0, inner/2), "…")
	rw := 0
	if right != "" {
		rw = lipgloss.Width(right) + 2
	}
	title = ansi.Truncate(title, max(0, inner-4-rw), "…")
	fill := max(0, inner-3-lipgloss.Width(title)-rw)
	top := b.Render("╭─ ") + ts.Render(title) + b.Render(" "+strings.Repeat("─", fill))
	if right != "" {
		top += " " + right + " "
	}
	top += b.Render("╮")

	var sb strings.Builder
	sb.WriteString(top)
	side := b.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		sb.WriteString("\n" + side + fit(line, inner) + side)
	}
	sb.WriteString("\n" + b.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return sb.String()
}

// fit truncates or pads s (which may contain ANSI) to exactly w cells.
func fit(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// part is a styled run of text inside a row.
type part struct {
	text string
	st   lipgloss.Style
}

func p(text string, st lipgloss.Style) part { return part{text, st} }

// row renders left- and right-aligned parts across width w. Selected rows get
// a background on every part, since an outer style would be reset by inner ones.
func row(w int, sel bool, focused bool, left []part, right []part) string {
	bg := lipgloss.NoColor{}
	var bgc lipgloss.TerminalColor = bg
	if sel {
		bgc = colSelBlur
		if focused {
			bgc = colSelFocus
		}
	}
	render := func(ps []part) string {
		var sb strings.Builder
		for _, x := range ps {
			st := x.st
			if sel {
				st = st.Background(bgc)
			}
			sb.WriteString(st.Render(x.text))
		}
		return sb.String()
	}
	r := render(right)
	l := render(left)
	rw := lipgloss.Width(r)
	if lipgloss.Width(l) > w-rw-1 {
		l = ansi.Truncate(l, max(0, w-rw-1), "…")
	}
	gap := max(0, w-lipgloss.Width(l)-rw)
	pad := strings.Repeat(" ", gap)
	if sel {
		pad = lipgloss.NewStyle().Background(bgc).Render(pad)
	}
	return l + pad + r
}

// wrap word-wraps s to w cells, hard-breaking long words.
func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	s = strings.ReplaceAll(strings.TrimRight(s, "\n"), "\t", "    ")
	return strings.Split(ansi.Wrap(s, w, ""), "\n")
}

func swatch(color string) lipgloss.Style {
	if color == "" {
		return stDim
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

// attention badges, by model.Attention* state.
var attentionBadge = map[string]struct {
	icon, label string
	color       lipgloss.Color
}{
	"permission": {"⚠", "needs permission", "#e06c75"},
	"question":   {"?", "asked a question", "#c678dd"},
	"notice":     {"!", "needs attention", "#e5c07b"},
	"input":      {"⏎", "awaiting input", "#61afef"},
}

func badge(att string) (string, lipgloss.Style) {
	b, ok := attentionBadge[att]
	if !ok {
		return "", lipgloss.NewStyle()
	}
	return b.icon, lipgloss.NewStyle().Foreground(b.color).Bold(true)
}

func statusStyle(status string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(statusPalette[status])
}

// blend mixes colour a into base by t (0..1); both #rrggbb. Returns base on bad input.
func blend(a, base string, t float64) string {
	pa, oka := parseHex(a)
	pb, okb := parseHex(base)
	if !oka || !okb {
		return base
	}
	var out [3]int
	for i := range out {
		out[i] = int(float64(pa[i])*t + float64(pb[i])*(1-t) + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

func parseHex(s string) ([3]int, bool) {
	var c [3]int
	if len(s) != 7 || s[0] != '#' {
		return c, false
	}
	_, err := fmt.Sscanf(s[1:], "%02x%02x%02x", &c[0], &c[1], &c[2])
	return c, err == nil
}

// tint paints an already-styled line on a background colour, padded to w.
// Styled segments end in a reset, which would clear an outer background, so
// the background is re-applied after every reset.
func tint(line string, w int, bg string) string {
	probe := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render("\x00")
	on, _, ok := strings.Cut(probe, "\x00")
	if !ok || on == "" { // no colour support: leave the line alone
		return line
	}
	line = fit(line, w)
	return on + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+on) + "\x1b[0m"
}
