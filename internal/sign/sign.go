package sign

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// Header is the Xylon Control webhook signature header name.
const Header = "x-xylon-signature"

// Sign returns sha256=<hex> HMAC-SHA256 of body (matches Xylon.Control.Webhooks.Delivery).
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Valid reports whether sig matches body and secret.
func Valid(body []byte, secret, sig string) bool {
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	expected := Sign(body, secret)
	a := []byte(expected)
	b := []byte(sig)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}
