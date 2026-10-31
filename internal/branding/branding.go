// Package branding holds Keystone's huisstijl: the name, logo and colours
// every page shows, set in Instellingen › Huisstijl.
package branding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Branding is the huisstijl.
type Branding struct {
	Name       string `json:"name"`
	Logo       string `json:"logo"`       // data:image/… URL; "" is Keystone's own mark
	Accent     string `json:"accent"`     // #rrggbb: buttons, links, the selected menu item
	Background string `json:"background"` // #rrggbb: the page behind the white boxes
	// LoginImage is the sign-in page's background photo (data:image/… URL);
	// "" is a soft gradient in the accent colour.
	LoginImage string `json:"login_image"`
}

// MaxLoginImage is the largest login background, as a data URL.
const MaxLoginImage = 4 << 20

// MaxName is the longest name, in characters.
const MaxName = 40

// Defaults is Keystone's own look.
func Defaults() Branding {
	return Branding{Name: "Keystone", Accent: "#0f6cbd", Background: "#f5f5f5"}
}

var hexRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// Normalize trims the name and lowercases the colours.
func (b *Branding) Normalize() {
	b.Name = strings.TrimSpace(b.Name)
	b.Accent = strings.ToLower(strings.TrimSpace(b.Accent))
	b.Background = strings.ToLower(strings.TrimSpace(b.Background))
}

// Check validates a normalized b except the logo (the caller vets it as an
// image). It returns the offending field and a message for the form.
func (b Branding) Check() (field, msg string) {
	switch n := utf8.RuneCountInString(b.Name); {
	case n == 0:
		return "name", "Enter a name."
	case n > MaxName:
		return "name", fmt.Sprintf("The name can be at most %d characters.", MaxName)
	case strings.IndexFunc(b.Name, unicode.IsControl) >= 0:
		return "name", "The name contains invalid characters."
	}
	if !hexRe.MatchString(b.Accent) {
		return "accent", "Choose a color."
	}
	// Buttons carry white text on the accent.
	if contrast(b.Accent, "#ffffff") < 3 {
		return "accent", "This color is too light: white text on buttons would be hard to read. Choose a darker color."
	}
	if !hexRe.MatchString(b.Background) {
		return "background", "Choose a color."
	}
	// The theme is light: dark text sits on the background.
	if luminance(b.Background) < 0.75 {
		return "background", "Choose a light background; text is dark on it."
	}
	return "", ""
}

// luminance is the WCAG relative luminance of a #rrggbb colour.
func luminance(hex string) float64 {
	var l [3]float64
	for i := range l {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			l[i] = c / 12.92
		} else {
			l[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
}

func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// Store keeps the huisstijl in Postgres (row id=1) with an in-memory cache;
// it assumes a single core instance.
type Store struct {
	pool  *pgxpool.Pool
	mu    sync.Mutex
	cache *Branding
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Get returns the huisstijl, Defaults() while none is saved.
func (s *Store) Get(ctx context.Context) (Branding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(ctx)
}

func (s *Store) loadLocked(ctx context.Context) (Branding, error) {
	if s.cache != nil {
		return *s.cache, nil
	}
	cur := Defaults()
	var raw []byte
	err := s.pool.QueryRow(ctx, `select data from branding where id = 1`).Scan(&raw)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Branding{}, err
	default:
		if err := json.Unmarshal(raw, &cur); err != nil {
			return Branding{}, err
		}
	}
	s.cache = &cur
	return cur, nil
}

// Put saves next (already checked) and returns the previous huisstijl.
func (s *Store) Put(ctx context.Context, next Branding) (Branding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.loadLocked(ctx)
	if err != nil {
		return Branding{}, err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return Branding{}, err
	}
	s.cache = nil
	if _, err := s.pool.Exec(ctx, `insert into branding (id, data, updated_at) values (1, $1, now())
		on conflict (id) do update set data = excluded.data, updated_at = excluded.updated_at`, raw); err != nil {
		return Branding{}, err
	}
	s.cache = &next
	return old, nil
}
