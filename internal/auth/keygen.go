package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"golang.org/x/crypto/pbkdf2"
)

const (
	keyBytes       = 32
	saltBytes      = 32
	pbkdfRounds    = 100000
	derivedKeySize = 32
)

// GenerateKey returns a new base64url API key.
func GenerateKey() (string, error) {
	buf := make([]byte, keyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// GenerateSalt returns a random base64url salt.
func GenerateSalt() (string, error) {
	buf := make([]byte, saltBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// GenerateID returns a compact random identifier.
func GenerateID(prefix string) (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}

// HashKey derives a PBKDF2-HMAC-SHA256 hash for a key and salt.
func HashKey(key, salt string) string {
	sum := pbkdf2.Key([]byte(key), []byte(salt), pbkdfRounds, derivedKeySize, sha256.New)
	return base64.RawURLEncoding.EncodeToString(sum)
}
