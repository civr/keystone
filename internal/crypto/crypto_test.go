package crypto

import (
	"bytes"
	"regexp"
	"testing"
)

func testKey() []byte { return bytes.Repeat([]byte{7}, 32) }

func TestSealOpenRoundTrip(t *testing.T) {
	sealed, err := Seal(testKey(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(testKey(), sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenTamperedFails(t *testing.T) {
	sealed, _ := Seal(testKey(), []byte("hello"))
	b := []byte(sealed)
	// flip a character in the middle of the base64 payload
	i := len(b) / 2
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	if _, err := Open(testKey(), string(b)); err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
	if _, err := Open(bytes.Repeat([]byte{8}, 32), sealed); err == nil {
		t.Fatal("expected error for wrong key")
	}
	if _, err := Open(testKey(), "!!notbase64"); err == nil {
		t.Fatal("expected error for bad base64")
	}
}

func TestVerifySecret(t *testing.T) {
	h, err := HashSecret("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\$argon2id\$v=19\$m=65536,t=3,p=2\$`).MatchString(h) {
		t.Fatalf("unexpected PHC string %q", h)
	}
	if !VerifySecret(h, "correct horse") {
		t.Fatal("right secret rejected")
	}
	if VerifySecret(h, "wrong") {
		t.Fatal("wrong secret accepted")
	}
	if VerifySecret("garbage", "x") {
		t.Fatal("garbage hash accepted")
	}
}

func TestRecoveryCodeFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := RecoveryCode()
		if !re.MatchString(c) {
			t.Fatalf("bad code %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Fatal("codes not random")
	}
}
