// Package loginform finds the login form on an app's page: where it posts
// to, which fields hold the username and password, and the other fields the
// app expects back (hidden tokens, a named submit button, a checked box).
// Keystone uses it to fill in a vault or gate app's sign-in by itself, and at
// sign-in time to send the fields the page asks for right then.
package loginform

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// MaxPage is the most of a page that is read.
const MaxPage = 1 << 20

// ErrNoForm means the page has no login form: no password field, or one the
// page's JavaScript sends itself.
var ErrNoForm = errors.New("loginform: no login form on the page")

// Field is a name and value the form sends.
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Form is a login form.
type Form struct {
	// Action is the absolute address the form posts to.
	Action string `json:"action"`
	// Method is GET or POST.
	Method    string `json:"method"`
	UserField string `json:"user_field"`
	PassField string `json:"pass_field"`
	// Extra holds the other fields the form sends, in page order.
	Extra []Field `json:"extra"`
	// Token reports a field that looks like an anti-forgery token.
	Token bool `json:"token"`
}

// Page is a fetched login page.
type Page struct {
	// URL is the page's address after redirects.
	URL string
	// Form is nil when the page has no login form.
	Form *Form
	// Script reports a page that is mostly JavaScript (a single-page app),
	// which builds its login form in the browser.
	Script bool
	// Cookies are the cookies the app set while serving the page, when the
	// client came from NewClient.
	Cookies []*http.Cookie
	// App is the app Keystone recognised, with how it signs in, or nil.
	App *Known

	body []byte
}

var tokenRe = regexp.MustCompile(`(?i)csrf|xsrf|token|nonce|authenticity|verification|__viewstate|__eventvalidation|_wpnonce`)

// userHint ranks names that are likely the username field.
var userHint = regexp.MustCompile(`(?i)user|login|email|e-mail|mail|account|uid|name|ident|benutzer|gebruiker`)

// Find returns the login form in page, which was served from base.
func Find(page []byte, base *url.URL) (*Form, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	b := *base
	baseURL := &b
	var forms []*html.Node
	byID := map[string]*html.Node{}
	var loose []*html.Node // inputs outside a form, tied to one by form="id"
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Base:
				if h := attr(n, "href"); h != "" {
					if u, err := baseURL.Parse(h); err == nil {
						baseURL = u
					}
				}
			case atom.Form:
				forms = append(forms, n)
				if id := attr(n, "id"); id != "" {
					byID[id] = n
				}
			case atom.Input, atom.Select, atom.Textarea, atom.Button:
				if attr(n, "form") != "" {
					loose = append(loose, n)
				}
			case atom.Template:
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	var best *Form
	bestScore := -1
	for _, f := range forms {
		controls := controlsOf(f)
		for _, l := range loose {
			if byID[attr(l, "form")] == f {
				controls = append(controls, l)
			}
		}
		form, score := read(f, controls, baseURL)
		if form != nil && score > bestScore {
			best, bestScore = form, score
		}
	}
	if best == nil {
		return nil, ErrNoForm
	}
	return best, nil
}

// controlsOf lists the form's own fields in page order.
func controlsOf(f *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				switch c.DataAtom {
				case atom.Input, atom.Select, atom.Textarea, atom.Button:
					if attr(c, "form") == "" {
						out = append(out, c)
					}
				case atom.Form, atom.Template:
					continue
				}
			}
			walk(c)
		}
	}
	walk(f)
	return out
}

// read turns one form into a login form, with a score: higher is a likelier
// sign-in form. It returns nil for forms without exactly one named password
// field (search boxes, sign-up and change-password forms).
func read(f *html.Node, controls []*html.Node, base *url.URL) (*Form, int) {
	pass := -1
	for i, c := range controls {
		if c.DataAtom == atom.Input && typeOf(c) == "password" {
			if pass >= 0 {
				return nil, 0 // two password fields: sign-up or change password
			}
			pass = i
		}
	}
	if pass < 0 || attr(controls[pass], "name") == "" {
		return nil, 0
	}
	// The username is the last text-like field before the password,
	// preferring one whose name says so.
	user := -1
	for i := pass - 1; i >= 0; i-- {
		c := controls[i]
		if c.DataAtom != atom.Input || attr(c, "name") == "" || hasAttr(c, "disabled") {
			continue
		}
		switch typeOf(c) {
		case "text", "email", "tel", "":
			if user < 0 || (!userHint.MatchString(attr(controls[user], "name")+attr(controls[user], "id")) && userHint.MatchString(attr(c, "name")+attr(c, "id"))) {
				user = i
			}
		}
	}
	if user < 0 { // some put the username after the password
		for i := pass + 1; i < len(controls); i++ {
			c := controls[i]
			if t := typeOf(c); c.DataAtom == atom.Input && attr(c, "name") != "" && (t == "text" || t == "email") && userHint.MatchString(attr(c, "name")+attr(c, "id")) {
				user = i
				break
			}
		}
	}

	form := &Form{Method: strings.ToUpper(attr(f, "method")), PassField: attr(controls[pass], "name")}
	if form.Method != http.MethodPost {
		form.Method = http.MethodGet
	}
	action := base
	if a := strings.TrimSpace(attr(f, "action")); a != "" && !strings.HasPrefix(strings.ToLower(a), "javascript:") {
		u, err := base.Parse(a)
		if err != nil {
			return nil, 0
		}
		action = u
	}
	if action.Scheme != "http" && action.Scheme != "https" {
		return nil, 0
	}
	a := *action
	a.Fragment = ""
	form.Action = a.String()
	if user >= 0 {
		form.UserField = attr(controls[user], "name")
	}

	submitted := false
	for i, c := range controls {
		name := attr(c, "name")
		if i == pass || i == user || name == "" || hasAttr(c, "disabled") {
			continue
		}
		switch c.DataAtom {
		case atom.Input:
			switch typeOf(c) {
			case "hidden":
				form.Extra = append(form.Extra, Field{name, attr(c, "value")})
			case "checkbox", "radio":
				if hasAttr(c, "checked") {
					v := attr(c, "value")
					if !hasAttr(c, "value") {
						v = "on"
					}
					form.Extra = append(form.Extra, Field{name, v})
				}
			case "submit", "image":
				if !submitted {
					form.Extra = append(form.Extra, Field{name, attr(c, "value")})
					submitted = true
				}
			case "button", "reset", "file", "password":
			default: // a second text field such as a domain or a captcha left empty
				form.Extra = append(form.Extra, Field{name, attr(c, "value")})
			}
		case atom.Button:
			if t := strings.ToLower(attr(c, "type")); (t == "" || t == "submit") && !submitted {
				form.Extra = append(form.Extra, Field{name, attr(c, "value")})
				submitted = true
			}
		case atom.Select:
			if v, ok := selected(c); ok {
				form.Extra = append(form.Extra, Field{name, v})
			}
		case atom.Textarea:
			form.Extra = append(form.Extra, Field{name, text(c)})
		}
	}
	for _, e := range form.Extra {
		if tokenRe.MatchString(e.Name) {
			form.Token = true
		}
	}

	score := 10
	if user >= 0 {
		score += 5
	}
	if form.Method == http.MethodPost {
		score += 3
	}
	if userHint.MatchString(attr(f, "id") + attr(f, "name") + attr(f, "action") + attr(f, "class")) {
		score += 2
	}
	return form, score
}

func selected(s *html.Node) (string, bool) {
	var first *html.Node
	var pick *html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.DataAtom == atom.Option {
				if first == nil {
					first = c
				}
				if pick == nil && hasAttr(c, "selected") {
					pick = c
				}
			}
			walk(c)
		}
	}
	walk(s)
	if pick == nil {
		pick = first
	}
	if pick == nil {
		return "", false
	}
	if hasAttr(pick, "value") {
		return attr(pick, "value"), true
	}
	return strings.TrimSpace(text(pick)), true
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			}
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func typeOf(n *html.Node) string { return strings.ToLower(strings.TrimSpace(attr(n, "type"))) }

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return true
		}
	}
	return false
}

// scripted reports a page that is mostly script with little markup: a
// single-page app that draws its login in the browser.
func scripted(page []byte) bool {
	l := bytes.ToLower(page)
	if bytes.Contains(l, []byte("type=\"password\"")) || bytes.Contains(l, []byte("type='password'")) || bytes.Contains(l, []byte("type=password")) {
		return false
	}
	return bytes.Contains(l, []byte("<script")) && (bytes.Contains(l, []byte(`id="root"`)) || bytes.Contains(l, []byte(`id="app"`)) ||
		bytes.Contains(l, []byte(`type="module"`)) || bytes.Contains(l, []byte("<noscript")) || bytes.Count(l, []byte("<script")) >= 3 || len(bytes.TrimSpace(stripTags(l))) < 200)
}

var tagRe = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]*>`)

func stripTags(b []byte) []byte { return tagRe.ReplaceAll(b, nil) }

// recorder is a cookie jar that also keeps every cookie as the app set it,
// with its attributes, so they can be handed on to the browser.
type recorder struct {
	*cookiejar.Jar
	mu  sync.Mutex
	set []*http.Cookie
}

func (r *recorder) SetCookies(u *url.URL, cs []*http.Cookie) {
	r.Jar.SetCookies(u, cs)
	r.mu.Lock()
	r.set = append(r.set, cs...)
	r.mu.Unlock()
}

// Recorded returns the cookies c's jar was given, if it was made by NewClient.
func Recorded(c *http.Client) []*http.Cookie {
	r, ok := c.Jar.(*recorder)
	if !ok {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*http.Cookie(nil), r.set...)
}

// NewClient fetches login pages and keeps their cookies. Apps on the home
// network often have self-signed certificates; nothing secret is sent with
// these requests, only the page is read.
func NewClient(timeout time.Duration) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Timeout: timeout,
		Jar:     &recorder{Jar: jar},
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // reads a login page; no credentials sent
			ResponseHeaderTimeout: timeout,
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// Fetch loads rawURL with c and finds its login form. When the page has none
// but links to a login page, that page is tried once.
func Fetch(ctx context.Context, c *http.Client, rawURL string) (*Page, error) {
	p, links, err := fetch(ctx, c, rawURL)
	if err != nil {
		return nil, err
	}
	first := p
	if p.Form == nil && !p.Script && len(links) > 0 {
		if q, _, err := fetch(ctx, c, links[0]); err == nil && q.Form != nil {
			p = q
		}
	}
	base, _ := url.Parse(first.URL)
	app, fp := identify(ctx, c, first.body, base, first.Script)
	if app == nil && p != first {
		app, fp = identify(ctx, c, p.body, base, false)
	}
	if app != nil && p.Form == nil && fp.loginPage != "" {
		u := *base
		u.Path, u.RawQuery, u.Fragment = fp.loginPage, "", ""
		if q, _, err := fetch(ctx, c, u.String()); err == nil && q.Form != nil {
			p = q
		}
	}
	p.App = app
	p.Cookies = Recorded(c)
	return p, nil
}

var loginLink = regexp.MustCompile(`(?i)(^|[/_\-.])(log-?in|sign-?in|signon|auth|aanmelden|inloggen|anmelden)([/_\-.?]|$)`)

func fetch(ctx context.Context, c *http.Client, rawURL string) (*Page, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, nil, fmt.Errorf("loginform: not an http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Keystone)")
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxPage))
	if err != nil {
		return nil, nil, err
	}
	final := resp.Request.URL
	p := &Page{URL: final.String(), body: body}
	if resp.StatusCode >= 500 {
		return nil, nil, fmt.Errorf("loginform: the app answered %d", resp.StatusCode)
	}
	p.Form, _ = Find(body, final)
	if p.Form == nil {
		p.Script = scripted(body)
	}
	var links []string
	if p.Form == nil {
		links = loginLinks(body, final)
	}
	return p, links, nil
}

// loginLinks lists same-host links that look like a login page.
func loginLinks(page []byte, base *url.URL) []string {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	var out []string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			if u, err := base.Parse(attr(n, "href")); err == nil && u.Host == base.Host && (u.Scheme == "http" || u.Scheme == "https") && loginLink.MatchString(u.Path) && u.Path != base.Path {
				u.Fragment = ""
				out = append(out, u.String())
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}
