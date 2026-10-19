package ad

import "strings"

// NormalizeUsername strips a DOMAIN\ prefix and an @suffix, lowercases and trims.
func NormalizeUsername(in string) string {
	s := strings.TrimSpace(in)
	if i := strings.LastIndex(s, `\`); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSpace(s))
}
