// Package onepassword reads items from a 1Password Connect server (the
// self-hosted API in front of 1Password vaults). Keystone stores only the
// Connect address and token (sealed) plus item references; secrets are
// fetched when used and never cached or logged.
package onepassword

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to one Connect server.
type Client struct {
	URL   string // e.g. http://op-connect:8080
	Token string // Connect access token
	HTTP  *http.Client
}

// Vault is a 1Password vault the token can read.
type Vault struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Summary is an item in a list (no secrets).
type Summary struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Vault    struct {
		ID string `json:"id"`
	} `json:"vault"`
}

// Secret is what Keystone uses of an item.
type Secret struct {
	Username, Password, Note, URL string
}

// ErrNotFound means the vault or item is gone or not readable by the token.
var ErrNotFound = errors.New("onepassword: not found")

func (c *Client) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := h.Do(req)
	if err != nil {
		return fmt.Errorf("onepassword: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden:
		return ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("onepassword: the Connect token was refused")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("onepassword: Connect answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// Vaults lists the vaults the token can read.
func (c *Client) Vaults(ctx context.Context) ([]Vault, error) {
	var v []Vault
	return v, c.get(ctx, "/v1/vaults", &v)
}

// Items lists the items of vault (titles only).
func (c *Client) Items(ctx context.Context, vault string) ([]Summary, error) {
	var v []Summary
	return v, c.get(ctx, "/v1/vaults/"+url.PathEscape(vault)+"/items", &v)
}

// Get returns the username, password, note and first URL of item in vault.
func (c *Client) Get(ctx context.Context, vault, item string) (Secret, error) {
	var raw struct {
		Fields []struct {
			Purpose string `json:"purpose"`
			Type    string `json:"type"`
			Label   string `json:"label"`
			Value   string `json:"value"`
		} `json:"fields"`
		URLs []struct {
			Href    string `json:"href"`
			Primary bool   `json:"primary"`
		} `json:"urls"`
	}
	if err := c.get(ctx, "/v1/vaults/"+url.PathEscape(vault)+"/items/"+url.PathEscape(item), &raw); err != nil {
		return Secret{}, err
	}
	var s Secret
	for _, f := range raw.Fields {
		switch {
		case f.Purpose == "USERNAME":
			s.Username = f.Value
		case f.Purpose == "PASSWORD":
			s.Password = f.Value
		case f.Purpose == "NOTES":
			s.Note = f.Value
		case s.Password == "" && f.Type == "CONCEALED":
			s.Password = f.Value // API keys, tokens: the first hidden field
		}
	}
	for _, u := range raw.URLs {
		if s.URL == "" || u.Primary {
			s.URL = u.Href
		}
	}
	return s, nil
}

// Ref is how Keystone points at an item: "<vault>/<item>".
func Ref(vault, item string) string { return vault + "/" + item }

// SplitRef undoes Ref.
func SplitRef(ref string) (vault, item string, ok bool) {
	vault, item, ok = strings.Cut(ref, "/")
	return vault, item, ok && vault != "" && item != ""
}
