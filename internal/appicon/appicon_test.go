package appicon

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func server(t *testing.T, routes map[string]func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.URL.Path]; ok {
			h(w)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPrefersAppleTouchIcon(t *testing.T) {
	icon := pngBytes(t, 32)
	srv := server(t, map[string]func(http.ResponseWriter){
		"/": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><link rel="icon" href="/small.png"><link rel="apple-touch-icon" sizes="180x180" href="static/touch.png"></head></html>`))
		},
		"/static/touch.png": func(w http.ResponseWriter) { w.Write(icon) },
		"/small.png":        func(w http.ResponseWriter) { w.Write(pngBytes(t, 8)) },
	})
	got, err := Fetch(context.Background(), srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(icon) {
		t.Fatalf("got %.60s…", got)
	}
}

func TestFallsBackToWellKnownPath(t *testing.T) {
	srv := server(t, map[string]func(http.ResponseWriter){
		"/":                     func(w http.ResponseWriter) { w.Write([]byte(`<html>no icons</html>`)) },
		"/apple-touch-icon.png": func(w http.ResponseWriter) { w.Write(pngBytes(t, 16)) },
	})
	got, err := Fetch(context.Background(), srv.URL+"/app/", nil)
	if err != nil || !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("got %.40s %v", got, err)
	}
}

func TestSVGAndRejections(t *testing.T) {
	srv := server(t, map[string]func(http.ResponseWriter){
		"/":      func(w http.ResponseWriter) { w.Write([]byte(`<link rel="icon" type="image/svg+xml" href="/i.svg">`)) },
		"/i.svg": func(w http.ResponseWriter) { w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`)) },
		"/bad/":  func(w http.ResponseWriter) { w.Write([]byte(`<link rel="icon" href="/bad/page.html">`)) },
		"/bad/page.html": func(w http.ResponseWriter) {
			w.Write([]byte(`<html>not an image</html>`))
		},
	})
	got, err := Fetch(context.Background(), srv.URL+"/", nil)
	if err != nil || !strings.HasPrefix(got, "data:image/svg+xml;base64,") {
		t.Fatalf("svg: %.40s %v", got, err)
	}
	if _, err := Fetch(context.Background(), srv.URL+"/bad/", nil); err != ErrNoIcon {
		t.Fatalf("html as icon: %v", err)
	}
	if _, err := Fetch(context.Background(), "ftp://example.com/", nil); err == nil {
		t.Fatal("non-http URL accepted")
	}
}

func TestTooLargeSkipped(t *testing.T) {
	big := pngBytes(t, 400) // noisy pixels: well over the limit
	srv := server(t, map[string]func(http.ResponseWriter){
		"/":                     func(w http.ResponseWriter) { w.Write([]byte(`<link rel="apple-touch-icon" href="/big.png">`)) },
		"/big.png":              func(w http.ResponseWriter) { w.Write(big) },
		"/apple-touch-icon.png": func(w http.ResponseWriter) { w.Write(pngBytes(t, 16)) },
	})
	got, err := Fetch(context.Background(), srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > MaxDataURL {
		t.Fatalf("data URL %d bytes", len(got))
	}
}
