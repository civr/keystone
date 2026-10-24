// Package appicon fetches an app's own icon (its apple-touch-icon, or a PNG,
// SVG or WebP icon its page links to) so a tile shows the real icon instead of
// a letter (spec §14: "automatisch favicon ophalen").
package appicon

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// MaxDataURL is the largest data URL an app tile accepts (apps.go maxIconLen).
const MaxDataURL = 64 << 10

const (
	maxPage = 512 << 10
	// maxIcon keeps the base64 data URL under MaxDataURL.
	maxIcon = (MaxDataURL - 64) / 4 * 3
)

// ErrNoIcon means the app offers no usable icon.
var ErrNoIcon = errors.New("appicon: no usable icon")

var (
	linkRe = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRe = regexp.MustCompile(`(?is)\b(rel|href|type)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
)

// client fetches from the home network: apps there often have self-signed
// certificates. Only images are taken from the answers, and nothing is sent.
var client = &http.Client{
	Timeout: 6 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // icon only; no credentials sent
		ResponseHeaderTimeout: 4 * time.Second,
	},
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// Fetch returns the app's icon as a data: URL. accept (optional) vets each
// candidate; a rejected one is skipped in favour of the next.
func Fetch(ctx context.Context, rawURL string, accept func(dataURL string) bool) (string, error) {
	base, err := url.Parse(rawURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return "", fmt.Errorf("appicon: not an http(s) URL")
	}
	var candidates []string
	if page, err := get(ctx, base.String(), maxPage); err == nil {
		candidates = linked(page)
	}
	candidates = append(candidates, "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png", "/favicon.svg", "/favicon.png")
	seen := map[string]bool{}
	for _, c := range candidates {
		ref, err := url.Parse(c)
		if err != nil {
			continue
		}
		u := base.ResolveReference(ref)
		if (u.Scheme != "http" && u.Scheme != "https") || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		body, err := get(ctx, u.String(), maxIcon+1)
		if err != nil || len(body) > maxIcon {
			continue
		}
		if mime := sniff(body); mime != "" {
			d := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body)
			if accept == nil || accept(d) {
				return d, nil
			}
		}
	}
	return "", ErrNoIcon
}

func get(ctx context.Context, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Keystone (app icon)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("appicon: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// linked returns icon hrefs from the page's <link> tags, best first:
// apple-touch-icon, then SVG icons, then other icons.
func linked(page []byte) []string {
	var touch, svg, other []string
	for _, tag := range linkRe.FindAll(page, 64) {
		var rel, href, typ string
		for _, m := range attrRe.FindAllSubmatch(tag, -1) {
			v := string(m[3]) + string(m[4]) + string(m[5])
			switch strings.ToLower(string(m[1])) {
			case "rel":
				rel = strings.ToLower(v)
			case "href":
				href = v
			case "type":
				typ = strings.ToLower(v)
			}
		}
		if href == "" {
			continue
		}
		switch {
		case strings.Contains(rel, "apple-touch-icon"):
			touch = append(touch, href)
		case strings.Contains(rel, "icon") && (typ == "image/svg+xml" || strings.HasSuffix(strings.ToLower(href), ".svg")):
			svg = append(svg, href)
		case strings.Contains(rel, "icon"):
			other = append(other, href)
		}
	}
	return append(append(touch, svg...), other...)
}

// sniff returns the image type for PNG, WebP or SVG bodies, else "".
func sniff(b []byte) string {
	switch ct := http.DetectContentType(b); ct {
	case "image/png", "image/webp":
		return ct
	}
	head := bytes.ToLower(bytes.TrimSpace(b[:min(len(b), 512)]))
	if bytes.HasPrefix(head, []byte("<svg")) || (bytes.HasPrefix(head, []byte("<?xml")) && bytes.Contains(head, []byte("<svg"))) {
		return "image/svg+xml"
	}
	return ""
}
