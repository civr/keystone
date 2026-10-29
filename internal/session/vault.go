package session

import "time"

// ConfirmedWithin reports whether sess was confirmed in the last d: by a
// passkey, or — while no passkey exists — by the password.
func ConfirmedWithin(sess Session, d time.Duration, now time.Time) bool {
	last := sess.LastPasskeyAt
	if sess.NoPasskey && sess.LastFullAuthAt.After(last) {
		last = sess.LastFullAuthAt
	}
	return !last.IsZero() && now.Sub(last) <= d
}

// VaultOpen reports whether the vault is unlocked in sess: confirmed within
// the vault's window, and after any manual lock.
// A window of 0 never asks again: signing in opens the vault for the
// session (until it is locked by hand).
func VaultOpen(sess Session, d time.Duration, now time.Time) bool {
	last := sess.LastPasskeyAt
	if sess.NoPasskey && sess.LastFullAuthAt.After(last) {
		last = sess.LastFullAuthAt
	}
	if d <= 0 {
		// Any real sign-in counts, password or passkey; a reverse-proxy
		// session has none and must confirm once.
		if sess.LastFullAuthAt.After(last) {
			last = sess.LastFullAuthAt
		}
		return !last.IsZero() && (sess.VaultLockedAt.IsZero() || last.After(sess.VaultLockedAt))
	}
	return ConfirmedWithin(sess, d, now) && last.After(sess.VaultLockedAt)
}
