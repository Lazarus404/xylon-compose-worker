package sign_test

import (
	"testing"

	"github.com/Lazarus404/xylon-compose-worker/internal/sign"
)

func TestSignMatchesKnownVector(t *testing.T) {
	body := []byte(`{"type":"recording.completed"}`)
	secret := "rec-wh-secret"
	sig := sign.Sign(body, secret)
	if !stringsHasPrefix(sig, "sha256=") {
		t.Fatalf("sig: %q", sig)
	}
	if !sign.Valid(body, secret, sig) {
		t.Fatal("expected valid")
	}
	if sign.Valid(body, "other", sig) {
		t.Fatal("expected invalid for other secret")
	}
	if sign.Valid([]byte(`{}`), secret, sig) {
		t.Fatal("expected invalid for other body")
	}
}

func stringsHasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
