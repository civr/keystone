package httpapi

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
)

// EmbeddedShell is the SPA shell (web/build/200.html) copied into
// internal/httpapi/shell/ at build time; the committed file is a placeholder.
//
//go:embed shell/200.html
var EmbeddedShell []byte

var (
	scriptRe = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
	srcRe    = regexp.MustCompile(`(?i)(^|\s)src\s*=`)
)

// CSP returns the Content-Security-Policy for shell: every inline <script>
// (one without src) is allowed by its SHA-256 hash. extra, if not blank, is
// appended as further directives.
func CSP(shell []byte, extra string) string {
	var b strings.Builder
	b.WriteString("default-src 'self'; script-src 'self'")
	for _, m := range scriptRe.FindAllSubmatch(shell, -1) {
		if srcRe.Match(m[1]) {
			continue
		}
		sum := sha256.Sum256(m[2])
		b.WriteString(" 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'")
	}
	b.WriteString("; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	return withExtra(b.String(), extra)
}

// withExtra appends the admin-supplied CSP directives (settings
// headers.csp_extra) to a base policy.
func withExtra(base, extra string) string {
	extra = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, extra)
	if extra = strings.Trim(extra, " ;"); extra != "" {
		return base + "; " + extra
	}
	return base
}

// serveShell answers every non-API GET/HEAD with the SPA shell; the client
// router takes it from there.
func (s *server) serveShell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		errMethodNotAllowed(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(s.shell)
	}
}
