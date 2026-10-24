package branding

import "testing"

func TestCheck(t *testing.T) {
	ok := Defaults()
	if f, m := ok.Check(); f != "" {
		t.Fatalf("defaults: %s %s", f, m)
	}
	for _, tc := range []struct {
		field string
		edit  func(*Branding)
	}{
		{"name", func(b *Branding) { b.Name = "" }},
		{"name", func(b *Branding) { b.Name = "12345678901234567890123456789012345678901" }},
		{"name", func(b *Branding) { b.Name = "a\nb" }},
		{"accent", func(b *Branding) { b.Accent = "blue" }},
		{"accent", func(b *Branding) { b.Accent = "#ffcc00" }}, // white text unreadable
		{"background", func(b *Branding) { b.Background = "#fff" }},
		{"background", func(b *Branding) { b.Background = "#1c1c1e" }}, // dark
	} {
		b := Defaults()
		tc.edit(&b)
		b.Normalize()
		if f, _ := b.Check(); f != tc.field {
			t.Errorf("%+v: field %q, want %q", b, f, tc.field)
		}
	}
	b := Branding{Name: "  Huis  ", Accent: "#0A7A3F", Background: "#FFFFFF"}
	b.Normalize()
	if f, m := b.Check(); f != "" || b.Name != "Huis" || b.Accent != "#0a7a3f" {
		t.Fatalf("%+v: %s %s", b, f, m)
	}
}
