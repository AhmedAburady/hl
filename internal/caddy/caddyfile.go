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

// ListHosts returns the address labels of all top-level site blocks.
func ListHosts(content string) []string {
	blocks := parseTopLevelBlocks(splitLines(content))
	hosts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.label != "" {
			hosts = append(hosts, b.label)
		}
	}
	return hosts
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
		if hasOpenBrace(stripComment(raw)) {
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

func hasOpenBrace(line string) bool {
	return strings.ContainsRune(line, '{')
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
