package httpapi

import (
	"bytes"
	"html"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/branding"
)

var (
	shellTitleRe = regexp.MustCompile(`(?is)<title>.*?</title>`)
	shellIconRe  = regexp.MustCompile(`(?is)<link\s+rel="icon"[^>]*>`)
	shellThemeRe = regexp.MustCompile(`(?is)<meta\s+name="theme-color"[^>]*>`)
)

// brandShell writes the huisstijl into the SPA shell, so the first paint
// already has the name, logo and colours: the title, the favicon, the
// theme-color, a <meta name="ks-brand"> the app reads the name from, and the
// two colour tokens every other colour derives from (theme.css).
func brandShell(shell []byte, b branding.Branding) []byte {
	name := html.EscapeString(b.Name)
	out := shellTitleRe.ReplaceAllLiteral(shell, []byte("<title>"+name+"</title>"))
	if b.Logo != "" {
		// iconRe-checked base64 data URL: no quotes or markup can occur.
		out = shellIconRe.ReplaceAllLiteral(out, []byte(`<link rel="icon" href="`+b.Logo+`" />`))
	}
	out = shellThemeRe.ReplaceAllLiteral(out, []byte(`<meta name="theme-color" content="`+b.Background+`" />`))
	head := `<meta name="ks-brand" content="` + name + `" />` +
		`<style id="ks-brand">:root{--primary:` + b.Accent + `;--color-bg:` + b.Background + `}</style></head>`
	return bytes.Replace(out, []byte("</head>"), []byte(head), 1)
}

func (s *server) getBranding(w http.ResponseWriter, r *http.Request) {
	b, err := s.branding.Get(r.Context())
	if err != nil {
		errInternal(w, "load branding", err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *server) putBranding(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var next branding.Branding
	if !readJSONLimit(w, r, &next, maxAppBody) {
		return
	}
	next.Normalize()
	if field, msg := next.Check(); field != "" {
		writeInvalid(w, field, msg)
		return
	}
	if next.Logo != "" {
		if msg := iconError(next.Logo); msg != "" {
			writeInvalid(w, "logo", msg)
			return
		}
	}
	old, err := s.branding.Put(ctx, next)
	if err != nil {
		errInternal(w, "save branding", err)
		return
	}
	// The logo is listed by whether it changed, not by its bytes.
	changed := map[string]any{}
	if old.Name != next.Name {
		changed["name"] = next.Name
	}
	if old.Accent != next.Accent {
		changed["accent"] = next.Accent
	}
	if old.Background != next.Background {
		changed["background"] = next.Background
	}
	if old.Logo != next.Logo {
		changed["logo"] = next.Logo != ""
	}
	if len(changed) > 0 {
		s.d.Audit.Log(ctx, audit.ActorOwner, "branding.changed", clientIP(ctx), changed)
	}
	writeJSON(w, http.StatusOK, next)
}

// shellFor returns the shell with the current huisstijl; the plain shell is
// Keystone's own look, also when the huisstijl cannot be loaded.
func (s *server) shellFor(r *http.Request) []byte {
	b, err := s.branding.Get(r.Context())
	if err != nil {
		slog.Warn("http: load branding", "err", err)
		return s.shell
	}
	if b == branding.Defaults() {
		return s.shell
	}
	return brandShell(s.shell, b)
}
