package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/civr/keystone/internal/branding"
)

var (
	photoRe      = regexp.MustCompile(`^data:image/(jpeg|png|webp);base64,[A-Za-z0-9+/]+={0,2}$`)
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
	bg := ""
	if b.LoginImage != "" {
		sum := sha256.Sum256([]byte(b.LoginImage))
		bg = `<meta name="ks-login-image" content="/api/branding/login-image?v=` + hex.EncodeToString(sum[:6]) + `" />`
	}
	head := bg + `<meta name="ks-brand" content="` + name + `" />` +
		`<style id="ks-brand">:root{--primary:` + b.Accent + `;--color-bg:` + b.Background + `}</style></head>`
	return bytes.Replace(out, []byte("</head>"), []byte(head), 1)
}

// loginImage serves the sign-in background (public: the sign-in page shows it).
func (s *server) loginImage(w http.ResponseWriter, r *http.Request) {
	b, err := s.branding.Get(r.Context())
	if err != nil {
		errInternal(w, "load branding", err)
		return
	}
	m := photoRe.FindStringSubmatch(b.LoginImage)
	if m == nil {
		errNotFound(w, r)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(b.LoginImage[strings.IndexByte(b.LoginImage, ',')+1:])
	if err != nil {
		errNotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/"+m[1])
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // the URL carries a hash
	_, _ = w.Write(raw)
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
	if !readJSONLimit(w, r, &next, branding.MaxLoginImage+maxAppBody) {
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
	if next.LoginImage != "" && !photoRe.MatchString(next.LoginImage) {
		writeInvalid(w, "login_image", "Choose a JPEG, PNG or WebP image.")
		return
	}
	if len(next.LoginImage) > branding.MaxLoginImage {
		writeInvalid(w, "login_image", "The image may be at most 3 MB.")
		return
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
	if old.LoginImage != next.LoginImage {
		changed["login_image"] = next.LoginImage != ""
	}
	if len(changed) > 0 {
		s.d.Audit.Log(ctx, actor(ctx), "branding.changed", clientIP(ctx), changed)
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
