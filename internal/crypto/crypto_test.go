package crypto

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"
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

func TestVerifySecretStrictPHC(t *testing.T) {
	h, _ := HashSecret("pw")
	if !VerifySecret(h, "pw") {
		t.Fatal("baseline failed")
	}
	parts := strings.Split(h, "$")
	with := func(i int, v string) string {
		c := append([]string(nil), parts...)
		c[i] = v
		return strings.Join(c, "$")
	}
	cases := map[string]string{
		"other memory":     with(3, "m=1048576,t=3,p=2"),
		"other time":       with(3, "m=65536,t=1,p=2"),
		"other threads":    with(3, "m=65536,t=3,p=1"),
		"trailing garbage": with(3, "m=65536,t=3,p=2,x=1"),
		"other version":    with(2, "v=16"),
		"short salt":       with(4, base64.RawStdEncoding.EncodeToString([]byte("short"))),
	}
	for name, hash := range cases {
		if VerifySecret(hash, "pw") {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRecoveryCodeFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){3}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		c := RecoveryCode()
		if !re.MatchString(c) {
			t.Fatalf("bad code %q", c)
		}
		seen[c] = true
	}
	if len(seen) != 1000 {
		t.Fatal("codes not random")
	}
}
