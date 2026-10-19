// Package secrets generates and loads the deployment secrets that live in a
// dedicated volume (mounted at /run/keystone). They are never stored in Postgres.
package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

// Files lists the secret file names Ensure manages.
var Files = []string{"db_password", "valkey_password", "session_key", "data_key"}

// Secrets holds the loaded deployment secrets.
type Secrets struct {
	DBPassword     string
	ValkeyPassword string
	SessionKey     []byte // 32 bytes
	DataKey        []byte // 32 bytes
}

func isPassword(name string) bool { return name == "db_password" || name == "valkey_password" }

// Ensure creates any missing secret file in dir with mode 0400 and never
// overwrites an existing one.
func Ensure(dir string) error {
	for _, name := range Files {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return fmt.Errorf("secrets: random: %w", err)
		}
		content := raw
		if isPassword(name) {
			content = []byte(base64.RawURLEncoding.EncodeToString(raw))
		}
		if err := writeNew(filepath.Join(dir, name), content); err != nil {
			return fmt.Errorf("secrets: %s: %w", name, err)
		}
	}
	return nil
}

func writeNew(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o400)
	if errors.Is(err, fs.ErrExist) {
		_, err := readSecret(filepath.Dir(path), filepath.Base(path))
		return err
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

var urlSafe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// readSecret reads and validates one secret file: it must be a regular file
// (not a symlink), keys must be exactly 32 bytes, passwords >=32 URL-safe chars.
func readSecret(dir, name string) ([]byte, error) {
	path := filepath.Join(dir, name)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read %s: %w", name, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("secrets: %s is not a regular file", name)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("secrets: read %s: %w", name, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return nil, fmt.Errorf("secrets: read %s: %w", name, err)
	}
	if isPassword(name) {
		if len(b) < 32 || !urlSafe.Match(b) {
			return nil, fmt.Errorf("secrets: %s must be at least 32 URL-safe characters", name)
		}
	} else if len(b) != 32 {
		return nil, fmt.Errorf("secrets: %s has %d bytes, want 32", name, len(b))
	}
	return b, nil
}

// Load reads and validates all secret files from dir.
func Load(dir string) (Secrets, error) {
	var s Secrets
	b, err := readSecret(dir, "db_password")
	if err != nil {
		return s, err
	}
	s.DBPassword = string(b)
	if b, err = readSecret(dir, "valkey_password"); err != nil {
		return s, err
	}
	s.ValkeyPassword = string(b)
	if s.SessionKey, err = readSecret(dir, "session_key"); err != nil {
		return s, err
	}
	if s.DataKey, err = readSecret(dir, "data_key"); err != nil {
		return s, err
	}
	return s, nil
}
