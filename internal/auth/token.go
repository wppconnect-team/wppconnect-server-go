// Package auth implements the Node server's session-bound bcrypt tokens.
package auth

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var encode = strings.NewReplacer("/", "_", "+", "-")
var decode = strings.NewReplacer("_", "/", "-", "+")

// Generate returns a URL-safe bcrypt hash of session + secret, as in Node.
func Generate(session, secret string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(session+secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return encode.Replace(string(hash)), nil
}

// Verify accepts both raw bcrypt hashes and the Node server's URL-safe form.
func Verify(session, secret, token string) error {
	return bcrypt.CompareHashAndPassword([]byte(decode.Replace(token)), []byte(session+secret))
}
