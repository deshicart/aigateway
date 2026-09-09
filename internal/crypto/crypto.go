// Package crypto provides AES-256-GCM encryption for provider keys,
// master-key management, secure random generation and password hashing
// using only the Go standard library (ARMv7-safe, no cgo).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const keyFileName = "master.key"

// LoadOrCreateMasterKey returns a 32-byte master key. It prefers the
// OPENBRIDGE_MASTER_KEY env var (raw 32 bytes or base64 of 32 bytes or
// an arbitrary passphrase stretched with SHA-256), otherwise loads
// dataDir/master.key or generates and persists one with 0600 perms.
func LoadOrCreateMasterKey(envKey, dataDir string) ([]byte, error) {
	if envKey != "" {
		if k, err := parseEnvKey(envKey); err == nil {
			return k, nil
		}
		// Fall through: treat as passphrase.
		sum := sha256.Sum256([]byte("openbridge-v1:" + envKey))
		return sum[:], nil
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dataDir, keyFileName)
	if b, err := os.ReadFile(p); err == nil {
		s := strings.TrimSpace(string(b))
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil || len(raw) != 32 {
			return nil, errors.New("corrupt master key file")
		}
		return raw, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(raw)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return raw, nil
}

func parseEnvKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s) == 32 {
		return []byte(s), nil
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err == nil && len(raw) == 32 {
		return raw, nil
	}
	raw, err = base64.URLEncoding.DecodeString(s)
	if err == nil && len(raw) == 32 {
		return raw, nil
	}
	return nil, errors.New("not a raw 32-byte key")
}

// Encrypt encrypts plaintext with AES-256-GCM, returning base64(nonce|ciphertext).
func Encrypt(master, plaintext []byte) (string, error) {
	gcm, err := newGCM(master)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt reverses Encrypt.
func Decrypt(master []byte, enc string) ([]byte, error) {
	gcm, err := newGCM(master)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

func newGCM(master []byte) (cipher.AEAD, error) {
	if len(master) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(master))
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// RandomKey generates a URL-safe secret of n random bytes (base64url, no padding).
func RandomKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b), nil
}

// NewGatewayKey returns an "obg_" prefixed gateway key.
func NewGatewayKey() (string, error) {
	s, err := RandomKey(32)
	if err != nil {
		return "", err
	}
	return "obg_" + s, nil
}

// HashGatewayKey returns sha256 hex of a gateway key for DB lookup.
func HashGatewayKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", sum)
}

// ---- password hashing (PBKDF2-HMAC-SHA256, stdlib only) ----

// HashPassword returns "pbkdf2$iter$saltB64$hashB64".
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const iter = 120_000 // conservative for low-end devices
	dk := pbkdf2([]byte(password), salt, iter, 32)
	return fmt.Sprintf("pbkdf2$%d$%s$%s",
		iter,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(dk)), nil
}

// VerifyPassword compares password against encoded hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	var iter int
	if _, err := fmt.Sscanf(parts[1], "%d", &iter); err != nil || iter <= 0 || iter > 2_000_000 {
		return false
	}
	salt, err1 := base64.StdEncoding.DecodeString(parts[2])
	want, err2 := base64.StdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got := pbkdf2([]byte(password), salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func pbkdf2(password, salt []byte, iter, keyLen int) []byte {
	hLen := sha256.Size
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for i := 1; i <= blocks; i++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for j := 1; j < iter; j++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for k := range t {
				t[k] ^= u[k]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

// Redact returns a safe prefix for logging (first 7 chars + ellipsis).
func Redact(secret string) string {
	if len(secret) <= 10 {
		return "***"
	}
	return secret[:7] + "…"
}
