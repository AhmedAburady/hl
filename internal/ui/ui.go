// Package ui centralizes terminal presentation: lipgloss styles, status
// messages, the styled DNS plan/record/host renderers, and a pretty slog
// handler. lipgloss auto-degrades to plain text when stdout is not a terminal,
// so piped output stays clean.
package ui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/AhmedAburady/hl/internal/reconcile"
	"github.com/AhmedAburady/hl/internal/technitium"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

var stdoutIsTTY = term.IsTerminal(int(os.Stdout.Fd()))

// hyperlink wraps text in an OSC 8 terminal hyperlink. On a non-TTY it returns
// text unchanged so piped output stays clean.
func hyperlink(url, text string) string {
	if !stdoutIsTTY {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

var (
	accent = lipgloss.Color("63")
	green  = lipgloss.Color("42")
	yellow = lipgloss.Color("214")
	orange = lipgloss.Color("208")
	red    = lipgloss.Color("203")
	blue   = lipgloss.Color("39")
	muted  = lipgloss.Color("243")

	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	accentStyle  = lipgloss.NewStyle().Foreground(accent)
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

// Step renders an in-progress action line ("▸ …").
func Step(format string, a ...any) string {
	return accentStyle.Render("▸ ") + fmt.Sprintf(format, a...)
}

// OK renders a success line ("✓ …").
func OK(format string, a ...any) string {
	return successStyle.Render("✓ ") + fmt.Sprintf(format, a...)
}

// Warn renders a cautionary line ("! …").
func Warn(format string, a ...any) string {
	return warnStyle.Render("! ") + fmt.Sprintf(format, a...)
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
	MarkUntracked
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
	case MarkUntracked:
		return "•"
	case MarkNA:
		return "—"
	case MarkUnknown:
		return "?"
	default:
		return ""
	}
}

func (m Mark) color() lipgloss.Color {
	switch m {
	case MarkOK:
		return green
	case MarkMissing:
		return red
	case MarkDrift:
		return yellow
	case MarkConflict:
		return orange
	case MarkUntracked:
		return blue
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
		{MarkUntracked, "untracked"},
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

// RenderRecords renders DNS records as one bordered table per zone, each under a
// zone heading. With all set, a leading "●" column marks (and green-tints) the
// records hl manages.
func RenderRecords(records []technitium.Record, all bool, tag string, local map[string]bool) string {
	byZone := map[string][]technitium.Record{}
	var zones []string
	for _, r := range records {
		if _, ok := byZone[r.Zone]; !ok {
			zones = append(zones, r.Zone)
		}
		byZone[r.Zone] = append(byZone[r.Zone], r)
	}
	sort.Strings(zones)

	var b strings.Builder
	for i, z := range zones {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(headingStyle.Render(z))
		b.WriteString("\n")
		b.WriteString(renderZoneTable(byZone[z], all, tag, local))
	}
	return b.String()
}

func renderZoneTable(records []technitium.Record, all bool, tag string, local map[string]bool) string {
	managed := make([]bool, len(records))
	rows := make([][]string, len(records))
	for i, r := range records {
		managed[i] = r.Comments == tag
		name := hyperlink("https://"+r.Name, r.Name)
		loc := ""
		if local[reconcile.NameKey(r.Name)] {
			loc = "✓"
		}
		row := []string{strconv.Itoa(i + 1)}
		if all {
			mark := ""
			if managed[i] {
				mark = "●"
			}
			row = append(row, mark)
		}
		row = append(row, name, r.Type, strconv.Itoa(r.TTL), recordValue(r), loc)
		rows[i] = row
	}

	headers := []string{"#"}
	if all {
		headers = append(headers, "")
	}
	headers = append(headers, "NAME", "TYPE", "TTL", "VALUE", "L")

	locCol := len(headers) - 1
	style := func(r, c int) lipgloss.Style {
		switch {
		case r == table.HeaderRow:
			return headerCellStyle
		case c == locCol && r >= 0 && r < len(records) && local[reconcile.NameKey(records[r].Name)]:
			return cellStyle.Align(lipgloss.Center).Foreground(green)
		case all && r >= 0 && r < len(managed) && managed[r]:
			return cellStyle.Foreground(green)
		default:
			return cellStyle
		}
	}
	return renderTable(headers, rows, style)
}

func recordValue(r technitium.Record) string {
	if r.RData == nil {
		return ""
	}
	for _, k := range []string{"ipAddress", "cname", "nameServer", "exchange", "text", "target"} {
		if v, ok := r.RData[k]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
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
	var c lipgloss.Color
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
