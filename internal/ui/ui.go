// Package ui centralizes terminal presentation: lipgloss styles, status
// messages, the styled DNS plan and host-status renderers, and a pretty slog
// handler.
package ui

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/AhmedAburady/hl/internal/reconcile"
	"golang.org/x/term"
)

var stdoutIsTTY = term.IsTerminal(int(os.Stdout.Fd()))

var (
	accent = lipgloss.Color("63")
	green  = lipgloss.Color("42")
	yellow = lipgloss.Color("214")
	orange = lipgloss.Color("208")
	red    = lipgloss.Color("203")
	blue   = lipgloss.Color("39")
	muted  = lipgloss.Color("243")

	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	successStyle = lipgloss.NewStyle().Foreground(green)
	warnStyle    = lipgloss.NewStyle().Foreground(yellow)
	mutedStyle   = lipgloss.NewStyle().Foreground(muted)

	createStyle   = lipgloss.NewStyle().Bold(true).Foreground(green)
	updateStyle   = lipgloss.NewStyle().Bold(true).Foreground(yellow)
	deleteStyle   = lipgloss.NewStyle().Bold(true).Foreground(red)
	conflictStyle = lipgloss.NewStyle().Bold(true).Foreground(orange)

	borderStyle     = lipgloss.NewStyle().Foreground(muted)
	headerCellStyle = lipgloss.NewStyle().Bold(true).Foreground(accent).Padding(0, 1)
	cellStyle       = lipgloss.NewStyle().Padding(0, 1)
	nameColStyle    = lipgloss.NewStyle().Width(34)
)

// Heading renders a bold accented title.
func Heading(format string, a ...any) string { return headingStyle.Render(fmt.Sprintf(format, a...)) }

// OK renders a success line ("✓ …").
func OK(format string, a ...any) string {
	return successStyle.Render("✓ ") + fmt.Sprintf(format, a...)
}

// Warn renders a cautionary line ("! …").
func Warn(format string, a ...any) string {
	return warnStyle.Render("! ") + fmt.Sprintf(format, a...)
}

func CheckLine(label string, width int, ok bool, reason string) string {
	mark := successStyle.Render("OK")
	if !ok {
		mark = lipgloss.NewStyle().Foreground(red).Render("FAIL")
	}
	line := fmt.Sprintf("%-*s [%s]", width, label, mark)
	if !ok && reason != "" {
		line += " " + mutedStyle.Render(reason)
	}
	return line
}

// Info renders a muted, secondary line.
func Info(format string, a ...any) string { return mutedStyle.Render(fmt.Sprintf(format, a...)) }

// Detail renders multi-line output (e.g. a remote command's logs) dimmed and
// indented so it reads as secondary context.
func Detail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = mutedStyle.Render("  " + l)
	}
	return strings.Join(lines, "\n")
}

// RenderPlan renders a reconcile plan as a colored diff (+ create, ~ update,
// - delete, ! conflict).
func RenderPlan(p reconcile.Plan) string {
	var b strings.Builder
	row := func(sigil string, st lipgloss.Style, a reconcile.Action, suffix string) {
		b.WriteString("  ")
		b.WriteString(st.Render(sigil))
		b.WriteString(" ")
		b.WriteString(st.Width(6).Render(string(a.Type)))
		b.WriteString(" ")
		b.WriteString(nameColStyle.Render(a.Domain))
		b.WriteString(" ")
		b.WriteString(mutedStyle.Render(a.Value))
		if suffix != "" {
			b.WriteString(" ")
			b.WriteString(mutedStyle.Render(suffix))
		}
		b.WriteString("\n")
	}
	for _, a := range p.Create {
		row("+", createStyle, a, "")
	}
	for _, a := range p.Update {
		row("~", updateStyle, a, "")
	}
	for _, a := range p.Delete {
		row("-", deleteStyle, a, "")
	}
	for _, a := range p.Conflict {
		row("!", conflictStyle, a, "(exists, not managed by hl)")
	}
	return strings.TrimRight(b.String(), "\n")
}

type Mark int

const (
	MarkNone Mark = iota
	MarkOK
	MarkMissing
	MarkDrift
	MarkConflict
	MarkNA
	MarkUnknown
)

func (m Mark) glyph() string {
	switch m {
	case MarkOK:
		return "✓"
	case MarkMissing:
		return "✗"
	case MarkDrift:
		return "~"
	case MarkConflict:
		return "!"
	case MarkNA:
		return "—"
	case MarkUnknown:
		return "?"
	default:
		return ""
	}
}

func (m Mark) color() color.Color {
	switch m {
	case MarkOK:
		return green
	case MarkMissing:
		return red
	case MarkDrift:
		return yellow
	case MarkConflict:
		return orange
	default:
		return muted
	}
}

func (m Mark) style() lipgloss.Style {
	return cellStyle.Align(lipgloss.Center).Foreground(m.color())
}

type StatusRow struct {
	Host              string
	Local, DNS, Caddy Mark
}

func termWidth() int {
	if !stdoutIsTTY {
		return 0
	}
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

func renderTable(headers []string, rows [][]string, style func(row, col int) lipgloss.Style) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(style)
	s := t.String()
	if w := termWidth(); w > 0 && lipgloss.Width(s) > w {
		return t.Width(w).Wrap(true).String()
	}
	return s
}

func RenderStatus(rows []StatusRow) string {
	trows := make([][]string, len(rows))
	for i, r := range rows {
		trows[i] = []string{strconv.Itoa(i + 1), r.Host, r.Local.glyph(), r.DNS.glyph(), r.Caddy.glyph()}
	}
	style := func(r, c int) lipgloss.Style {
		switch {
		case r == table.HeaderRow:
			return headerCellStyle
		case c == 2:
			return rows[r].Local.style()
		case c == 3:
			return rows[r].DNS.style()
		case c == 4:
			return rows[r].Caddy.style()
		default:
			return cellStyle
		}
	}
	return renderTable([]string{"#", "HOST", "L", "DNS", "CA"}, trows, style)
}

func StatusLegend(rows []StatusRow) string {
	present := map[Mark]bool{}
	for _, r := range rows {
		present[r.Local] = true
		present[r.DNS] = true
		present[r.Caddy] = true
	}
	notable := []struct {
		m     Mark
		label string
	}{
		{MarkMissing, "missing"},
		{MarkDrift, "drift"},
		{MarkConflict, "conflict"},
		{MarkUnknown, "unknown"},
	}
	var parts []string
	for _, n := range notable {
		if present[n.m] {
			glyph := lipgloss.NewStyle().Foreground(n.m.color()).Render(n.m.glyph())
			parts = append(parts, glyph+mutedStyle.Render(" "+n.label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, mutedStyle.Render("   "))
}

// LogHandler is a slog.Handler that renders records as a styled level badge plus
// message and dimmed key=value attrs, replacing slog's default text output.
type LogHandler struct {
	w     io.Writer
	level slog.Level
	attrs []slog.Attr
}

// NewLogHandler returns a styled slog handler writing to w at or above level.
func NewLogHandler(w io.Writer, level slog.Level) *LogHandler {
	return &LogHandler{w: w, level: level}
}

func (h *LogHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *LogHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(levelBadge(r.Level))
	b.WriteString(" ")
	b.WriteString(r.Message)
	for _, a := range h.attrs {
		b.WriteString(" " + mutedStyle.Render(a.Key+"="+a.Value.String()))
	}
	r.Attrs(func(a slog.Attr) bool {
		b.WriteString(" " + mutedStyle.Render(a.Key+"="+a.Value.String()))
		return true
	})
	b.WriteString("\n")
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	nh.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &nh
}

func (h *LogHandler) WithGroup(string) slog.Handler { return h }

func levelBadge(l slog.Level) string {
	var c color.Color
	var label string
	switch {
	case l >= slog.LevelError:
		c, label = red, "ERROR"
	case l >= slog.LevelWarn:
		c, label = yellow, "WARN"
	case l >= slog.LevelInfo:
		c, label = blue, "INFO"
	default:
		c, label = muted, "DEBUG"
	}
	return lipgloss.NewStyle().Bold(true).Foreground(c).Render(label)
}
