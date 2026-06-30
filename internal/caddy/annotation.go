package caddy

import (
	"fmt"
	"strconv"
	"strings"
)

// DNSAnnotation is the DNS intent declared in a comment directly above a site
// block, e.g. `# dsm type=CNAME zone=synology.com`. The leading bare word is the
// record's short name; the remaining tokens are key=value attributes. A block
// with no such directive has Present == false and is not managed for DNS.
type DNSAnnotation struct {
	Name    string // record short name (leading bare word)
	Type    string // A or CNAME (empty => inferred downstream)
	Zone    string // authoritative zone (empty => config default)
	Value   string // record value (empty => config default by type)
	TTL     int    // seconds (0 => server default)
	Present bool
}

// annotationKeys are the recognized key=value attributes. A directive line must
// contain at least one of these to be detected as a DNS directive (rather than a
// prose comment that merely contains '=').
var annotationKeys = map[string]bool{
	"type":  true,
	"zone":  true,
	"value": true,
	"ttl":   true,
}

// parseDirectiveLine attempts to parse a single comment's content (the text
// after the leading '#') as a DNS directive. It returns ok == false when the
// line is ordinary prose. A line that is detected as a directive but contains an
// unknown key or malformed ttl yields an error (typo protection).
func parseDirectiveLine(text string) (DNSAnnotation, bool, error) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return DNSAnnotation{}, false, nil
	}
	name := fields[0]
	if strings.Contains(name, "=") {
		return DNSAnnotation{}, false, nil // first token must be a bare word
	}

	// Detection: at least one recognized key=value among the remaining tokens.
	recognized := false
	for _, f := range fields[1:] {
		k, _, ok := strings.Cut(f, "=")
		if ok && annotationKeys[strings.ToLower(k)] {
			recognized = true
			break
		}
	}
	if !recognized {
		return DNSAnnotation{}, false, nil
	}

	a := DNSAnnotation{Name: name, Present: true}
	for _, f := range fields[1:] {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			return DNSAnnotation{}, true, fmt.Errorf("malformed token %q in directive %q (want key=value)", f, text)
		}
		switch strings.ToLower(k) {
		case "type":
			a.Type = strings.ToUpper(v)
		case "zone":
			a.Zone = v
		case "value":
			a.Value = v
		case "ttl":
			n, err := strconv.Atoi(v)
			if err != nil {
				return DNSAnnotation{}, true, fmt.Errorf("invalid ttl %q in directive %q", v, text)
			}
			a.TTL = n
		default:
			return DNSAnnotation{}, true, fmt.Errorf("unknown key %q in directive %q", k, text)
		}
	}
	return a, true, nil
}

// parseAnnotation scans a group of comment lines (the contiguous comments
// directly above a block) and returns the single DNS directive among them. Two
// directives in one group is an error.
func parseAnnotation(commentLines []string) (DNSAnnotation, error) {
	var found DNSAnnotation
	for _, line := range commentLines {
		text, ok := commentText(line)
		if !ok {
			continue
		}
		a, isDirective, err := parseDirectiveLine(text)
		if err != nil {
			return DNSAnnotation{}, err
		}
		if !isDirective {
			continue
		}
		if found.Present {
			return DNSAnnotation{}, fmt.Errorf("multiple DNS directives above block (%q and %q)", found.Name, a.Name)
		}
		found = a
	}
	return found, nil
}

// commentText returns the text after the leading '#' of a comment line, and
// whether the line is a comment at all.
func commentText(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "#") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "#")), true
}
