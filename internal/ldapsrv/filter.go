package ldapsrv

import (
	"strings"

	ber "github.com/go-asn1-ber/asn1-ber"
)

// Filter choice tags (RFC 4511 §4.5.1).
const (
	filterAnd         = 0
	filterOr          = 1
	filterNot         = 2
	filterEquality    = 3
	filterSubstrings  = 4
	filterGreater     = 5
	filterLess        = 6
	filterPresent     = 7
	filterApprox      = 8
	filterExtensible  = 9
	maxFilterChildren = 64
)

// match evaluates an encoded search filter against e. Comparisons are
// case-insensitive (every attribute here has caseIgnore semantics, DNs
// included). Ordering and extensible matches never match.
func match(f *ber.Packet, e entry, depth int) bool {
	if f == nil || depth > 16 || f.ClassType != ber.ClassContext {
		return false
	}
	switch f.Tag {
	case filterAnd:
		if len(f.Children) > maxFilterChildren {
			return false
		}
		for _, c := range f.Children {
			if !match(c, e, depth+1) {
				return false
			}
		}
		return true
	case filterOr:
		if len(f.Children) > maxFilterChildren {
			return false
		}
		for _, c := range f.Children {
			if match(c, e, depth+1) {
				return true
			}
		}
		return false
	case filterNot:
		return len(f.Children) == 1 && !match(f.Children[0], e, depth+1)
	case filterPresent:
		if strings.EqualFold(str(f), "objectClass") {
			return true
		}
		return len(e.get(str(f))) > 0
	case filterEquality, filterApprox:
		if len(f.Children) != 2 {
			return false
		}
		want := norm(str(f.Children[1]))
		for _, v := range e.get(str(f.Children[0])) {
			if norm(v) == want {
				return true
			}
		}
		return false
	case filterSubstrings:
		if len(f.Children) != 2 {
			return false
		}
		for _, v := range e.get(str(f.Children[0])) {
			if substrings(norm(v), f.Children[1].Children) {
				return true
			}
		}
		return false
	}
	return false
}

// substrings matches initial/any/final parts (context tags 0/1/2) in order.
func substrings(v string, parts []*ber.Packet) bool {
	for i, p := range parts {
		s := norm(str(p))
		switch p.Tag {
		case 0:
			if i != 0 || !strings.HasPrefix(v, s) {
				return false
			}
			v = v[len(s):]
		case 1:
			j := strings.Index(v, s)
			if j < 0 {
				return false
			}
			v = v[j+len(s):]
		case 2:
			if i != len(parts)-1 || !strings.HasSuffix(v, s) {
				return false
			}
			v = ""
		default:
			return false
		}
	}
	return true
}

// norm lowercases and drops spaces around DN separators.
func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.ContainsAny(s, ",=") {
		parts := strings.Split(s, ",")
		for i, p := range parts {
			kv := strings.SplitN(p, "=", 2)
			for j := range kv {
				kv[j] = strings.TrimSpace(kv[j])
			}
			parts[i] = strings.Join(kv, "=")
		}
		s = strings.Join(parts, ",")
	}
	return s
}
