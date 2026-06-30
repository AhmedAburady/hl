package caddy

import (
	"fmt"
	"strings"
)

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

func blockUpstream(lines []string, b siteBlock) string {
	relDepth := 0
	for i := b.openLine + 1; i < b.closeLine; i++ {
		braces := braceRunes(lines[i])
		if relDepth == 0 {
			stripped := stripComment(lines[i])
			if firstField(stripped) == "reverse_proxy" {
				if fields := strings.Fields(stripped); len(fields) >= 2 && fields[1] != "{" {
					return fields[1]
				}
				if endsWithOpenBrace(stripped) {
					return firstToUpstream(lines, i+1, b.closeLine)
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

func firstToUpstream(lines []string, start, end int) string {
	depth := 0
	for i := start; i < end; i++ {
		stripped := stripComment(lines[i])
		if depth == 0 && firstField(stripped) == "to" {
			if fields := strings.Fields(stripped); len(fields) >= 2 {
				return fields[1]
			}
		}
		if depth += countBraces(braceRunes(lines[i])); depth < 0 {
			return ""
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

// splitLines splits on \n and drops a trailing empty element produced by a
// final newline so round-tripping is stable.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
