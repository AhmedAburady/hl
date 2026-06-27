package caddy

import (
	"errors"
	"fmt"
	"strings"
)

// ErrReverseProxyBlock is returned when an existing site block uses the
// block form of reverse_proxy and the caller did not pass force.
var ErrReverseProxyBlock = errors.New("existing reverse_proxy is a block; pass --force to replace")

// UpsertResult describes what UpsertReverseProxy did.
type UpsertResult struct {
	Changed  bool
	Updated  bool // updated an existing block
	Created  bool // appended a new block
	Host     string
	Upstream string
}

// UpsertReverseProxy ensures a site block for host exists in content with a
// single-line `reverse_proxy <upstream>` directive. If a block for host
// already exists, its simple reverse_proxy directive is updated in place; if
// it uses the block form, an error is returned unless force replaces it. Other
// directives in the block are preserved.
func UpsertReverseProxy(content, host, upstream string, force bool) (string, *UpsertResult, error) {
	host = strings.TrimSpace(host)
	upstream = strings.TrimSpace(upstream)
	if host == "" {
		return "", nil, errors.New("host is required")
	}
	if upstream == "" {
		return "", nil, errors.New("upstream is required")
	}

	lines := splitLines(content)
	blocks := parseTopLevelBlocks(lines)

	idx := findBlock(blocks, host)
	res := &UpsertResult{Host: host, Upstream: upstream}

	if idx >= 0 {
		out, changed, err := updateBlock(lines, blocks[idx], upstream, force)
		if err != nil {
			return "", nil, err
		}
		res.Updated = true
		res.Changed = changed
		return joinLines(out), res, nil
	}

	out := appendNewBlock(lines, host, upstream)
	res.Created = true
	res.Changed = true
	return joinLines(out), res, nil
}

// Site is a parsed top-level site block: its host label, the single-line
// reverse_proxy upstream (empty if none or block-form), and the DNS directive
// declared in the comment group directly above it.
type Site struct {
	Host     string
	Upstream string
	DNS      DNSAnnotation
}

// ParseSites returns every top-level site block with its host, upstream, and any
// DNS directive. It is the read path for sync/status. An invalid directive above
// a block is a hard error.
func ParseSites(content string) ([]Site, error) {
	lines := splitLines(content)
	blocks := parseTopLevelBlocks(lines)
	sites := make([]Site, 0, len(blocks))
	for _, b := range blocks {
		if b.label == "" {
			continue
		}
		a, err := parseAnnotation(commentGroupAbove(lines, b.openLine))
		if err != nil {
			return nil, fmt.Errorf("site %s: %w", b.label, err)
		}
		sites = append(sites, Site{
			Host:     b.label,
			Upstream: blockUpstream(lines, b),
			DNS:      a,
		})
	}
	return sites, nil
}

// UpsertDNSAnnotation inserts or replaces the DNS directive line in the comment
// group directly above host's block. Other comments in the group are preserved.
func UpsertDNSAnnotation(content, host string, a DNSAnnotation) (string, error) {
	host = strings.TrimSpace(host)
	if !a.Present || strings.TrimSpace(a.Name) == "" {
		return "", errors.New("DNS annotation requires a record name")
	}
	lines := splitLines(content)
	blocks := parseTopLevelBlocks(lines)
	idx := findBlock(blocks, host)
	if idx < 0 {
		return "", fmt.Errorf("no site block for host %s", host)
	}
	directive := formatDNSAnnotation(a)

	// Find an existing directive line within the comment group above the block.
	start := commentGroupStart(lines, blocks[idx].openLine)
	for i := start; i < blocks[idx].openLine; i++ {
		text, ok := commentText(lines[i])
		if !ok {
			continue
		}
		if _, isDirective, _ := parseDirectiveLine(text); isDirective {
			out := append([]string(nil), lines...)
			out[i] = directive
			return joinLines(out), nil
		}
	}

	// None present: insert directly above the opening line.
	out := append([]string(nil), lines[:blocks[idx].openLine]...)
	out = append(out, directive)
	out = append(out, lines[blocks[idx].openLine:]...)
	return joinLines(out), nil
}

// commentGroupStart returns the index of the first line in the contiguous run of
// comment lines ending just above openLine (== openLine if there are none).
func commentGroupStart(lines []string, openLine int) int {
	start := openLine
	for j := openLine - 1; j >= 0; j-- {
		if _, ok := commentText(lines[j]); !ok {
			break
		}
		start = j
	}
	return start
}

// commentGroupAbove returns the contiguous comment lines directly above openLine.
func commentGroupAbove(lines []string, openLine int) []string {
	start := commentGroupStart(lines, openLine)
	return lines[start:openLine]
}

// blockUpstream returns the argument of a top-level single-line reverse_proxy
// directive in the block, or "" if none (or block-form).
func blockUpstream(lines []string, b siteBlock) string {
	relDepth := 0
	for i := b.openLine + 1; i < b.closeLine; i++ {
		braces := braceRunes(lines[i])
		if relDepth == 0 {
			stripped := stripComment(lines[i])
			if firstField(stripped) == "reverse_proxy" && !endsWithOpenBrace(stripped) {
				fields := strings.Fields(stripped)
				if len(fields) >= 2 {
					return fields[1]
				}
				return ""
			}
		}
		relDepth += countBraces(braces)
		if relDepth < 0 {
			relDepth = 0
		}
	}
	return ""
}

// ----- parsing -----

type siteBlock struct {
	label     string
	openLine  int // index of line containing the opening {
	closeLine int // index of line containing the matching }
}

// braceRunes returns the sequence of { and } in a line, ignoring quoted
// strings and trailing # comments.
func braceRunes(line string) []byte {
	var out []byte
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQuote {
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case '#':
			return out
		case '{', '}':
			out = append(out, c)
		}
	}
	return out
}

func parseTopLevelBlocks(lines []string) []siteBlock {
	var blocks []siteBlock
	depth := 0
	openLine := -1
	for i, line := range lines {
		for _, r := range braceRunes(line) {
			if r == '{' {
				if depth == 0 {
					openLine = i
				}
				depth++
			} else {
				depth--
				if depth == 0 && openLine >= 0 {
					b := siteBlock{openLine: openLine, closeLine: i}
					b.label = deriveLabel(lines, openLine)
					blocks = append(blocks, b)
					openLine = -1
				}
			}
		}
	}
	return blocks
}

func deriveLabel(lines []string, openIdx int) string {
	line := lines[openIdx]
	if before, _, ok := strings.Cut(line, "{"); ok {
		if before := strings.TrimSpace(before); before != "" {
			return normalizeLabel(before)
		}
	}
	for j := openIdx - 1; j >= 0; j-- {
		t := strings.TrimSpace(stripComment(lines[j]))
		if t != "" {
			return normalizeLabel(t)
		}
	}
	return ""
}

func stripComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQuote {
			if c == '"' {
				inQuote = false
			}
			continue
		}
		if c == '"' {
			inQuote = true
			continue
		}
		if c == '#' {
			return line[:i]
		}
	}
	return line
}

func normalizeLabel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ','
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], "\"")
}

func findBlock(blocks []siteBlock, host string) int {
	for i, b := range blocks {
		if b.label == host {
			return i
		}
	}
	return -1
}

// ----- editing -----

// updateBlock updates the reverse_proxy directive inside an existing block.
func updateBlock(lines []string, b siteBlock, upstream string, force bool) ([]string, bool, error) {
	// Find a top-level (relative depth 0) reverse_proxy directive in the body.
	relDepth := 0
	rpLine := -1
	for i := b.openLine + 1; i < b.closeLine; i++ {
		braces := braceRunes(lines[i])
		// A directive at relDepth 0 must be evaluated before counting braces
		// on this line that belong to a nested block.
		if relDepth == 0 {
			if first := firstField(stripComment(lines[i])); first == "reverse_proxy" {
				rpLine = i
				break
			}
		}
		relDepth += countBraces(braces)
		if relDepth < 0 {
			relDepth = 0
		}
	}

	newDirective := "reverse_proxy " + upstream

	if rpLine >= 0 {
		raw := lines[rpLine]
		if endsWithOpenBrace(stripComment(raw)) {
			// block form: reverse_proxy { ... }
			if !force {
				return nil, false, fmt.Errorf("%w (host %s)", ErrReverseProxyBlock, b.label)
			}
			return replaceReverseProxyBlock(lines, rpLine, newDirective)
		}
		indent := leadingIndent(raw)
		proposed := indent + newDirective
		if raw == proposed {
			return lines, false, nil
		}
		out := append([]string(nil), lines...)
		out[rpLine] = proposed
		return out, true, nil
	}

	// No reverse_proxy directive: insert one right after the opening line.
	indent := bodyIndent(lines, b)
	out := append([]string(nil), lines[:b.openLine+1]...)
	out = append(out, indent+newDirective)
	out = append(out, lines[b.openLine+1:]...)
	return out, true, nil
}

// replaceReverseProxyBlock replaces the block-form reverse_proxy (from rpLine
// to its matching close brace) with a single-line directive.
func replaceReverseProxyBlock(lines []string, rpLine int, newDirective string) ([]string, bool, error) {
	depth := 0
	end := -1
	for i := rpLine; i < len(lines); i++ {
		braces := braceRunes(lines[i])
		// If this is the first line, the directive keyword precedes the {.
		depth += countBraces(braces)
		if depth <= 0 {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, false, errors.New("unbalanced reverse_proxy block")
	}
	indent := leadingIndent(lines[rpLine])
	out := append([]string(nil), lines[:rpLine]...)
	out = append(out, indent+newDirective)
	out = append(out, lines[end+1:]...)
	return out, true, nil
}

func appendNewBlock(lines []string, host, upstream string) []string {
	block := []string{
		host + " {",
		"\treverse_proxy " + upstream,
		"}",
	}
	out := append([]string(nil), lines...)
	// ensure a blank line separates the new block from preceding content
	if len(out) > 0 {
		last := strings.TrimSpace(out[len(out)-1])
		if last != "" {
			out = append(out, "")
		}
	}
	out = append(out, block...)
	return out
}

// ----- helpers -----

func firstField(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// endsWithOpenBrace reports whether a directive line opens a block, i.e. its
// last non-space token is `{`. This distinguishes the block form
// `reverse_proxy ... {` from a single-line directive whose argument merely
// contains a brace (e.g. a `{placeholder}` or `{$ENV}` upstream).
func endsWithOpenBrace(line string) bool {
	return strings.HasSuffix(strings.TrimSpace(line), "{")
}

func countBraces(braces []byte) int {
	n := 0
	for _, r := range braces {
		if r == '{' {
			n++
		} else {
			n--
		}
	}
	return n
}

func leadingIndent(line string) string {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return line[:i]
		}
	}
	return line
}

// bodyIndent picks a sensible indent for new directives: the indent of the
// first non-empty body line, or a tab otherwise.
func bodyIndent(lines []string, b siteBlock) string {
	for i := b.openLine + 1; i < b.closeLine; i++ {
		t := strings.TrimSpace(stripComment(lines[i]))
		if t != "" {
			return leadingIndent(lines[i])
		}
	}
	return "\t"
}

// splitLines splits on \n and drops a trailing empty element produced by a
// final newline so round-tripping is stable.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}
