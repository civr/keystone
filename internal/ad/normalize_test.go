package ad

import "testing"

func TestNormalizeUsername(t *testing.T) {
	for in, want := range map[string]string{
		"THIJS":          "thijs",
		`HOME\thijs`:     "thijs",
		"thijs@home.lan": "thijs",
		" Thijs ":        "thijs",
	} {
		if got := NormalizeUsername(in); got != want {
			t.Errorf("NormalizeUsername(%q) = %q, want %q", in, got, want)
		}
	}
}
