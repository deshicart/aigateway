package crypto_test

import (
	"strings"
	"testing"

	"github.com/openbridge/gateway/internal/crypto"
)

func TestEncryptRoundTrip(t *testing.T) {
	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i)
	}
	enc, err := crypto.Encrypt(master, []byte("sk-secret-key"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, "sk-secret") {
		t.Fatal("ciphertext leaks plaintext")
	}
	dec, err := crypto.Decrypt(master, enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != "sk-secret-key" {
		t.Fatalf("round trip mismatch: %q", dec)
	}
}

func TestEncryptWrongKeyFails(t *testing.T) {
	m1 := make([]byte, 32)
	m2 := make([]byte, 32)
	m2[0] = 1
	enc, _ := crypto.Encrypt(m1, []byte("x"))
	if _, err := crypto.Decrypt(m2, enc); err == nil {
		t.Fatal("decrypt with wrong key must fail (GCM auth)")
	}
}

func TestGatewayKeyFormat(t *testing.T) {
	k, err := crypto.NewGatewayKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k, "obg_") {
		t.Fatalf("bad prefix: %s", k)
	}
	h1 := crypto.HashGatewayKey(k)
	h2 := crypto.HashGatewayKey(k)
	if h1 != h2 || len(h1) != 64 {
		t.Fatal("hash unstable")
	}
	if strings.Contains(h1, k) {
		t.Fatal("hash leaks key")
	}
}

func TestPasswordHashVerify(t *testing.T) {
	h, err := crypto.HashPassword("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h, "correct-horse") {
		t.Fatal("hash leaks password")
	}
	if !crypto.VerifyPassword("correct-horse", h) {
		t.Fatal("valid password rejected")
	}
	if crypto.VerifyPassword("wrong", h) {
		t.Fatal("invalid password accepted")
	}
}

func TestMasterKeyFromEnv(t *testing.T) {
	k, err := crypto.LoadOrCreateMasterKey("test-passphrase-for-unit-tests", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 32 {
		t.Fatal("master key must be 32 bytes")
	}
}
