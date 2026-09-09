// Package auth manages gateway API keys (obg_), admin password and sessions.
package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/openbridge/gateway/internal/crypto"
	"github.com/openbridge/gateway/internal/database"
)

type GatewayKey struct {
	ID         string
	Name       string
	KeyHash    string
	KeyPrefix  string
	CreatedAt  string
	LastUsedAt string
}

func CreateGatewayKey(db *sql.DB, name string) (plain string, rec GatewayKey, err error) {
	plain, err = crypto.NewGatewayKey()
	if err != nil {
		return "", rec, err
	}
	h := crypto.HashGatewayKey(plain)
	prefix := plain
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	id, err := crypto.RandomKey(12)
	if err != nil {
		return "", rec, err
	}
	now := database.Now()
	_, err = db.Exec(`INSERT INTO gateway_keys(id,name,key_hash,key_prefix,created_at) VALUES(?,?,?,?,?)`, id, name, h, prefix, now)
	if err != nil {
		return "", rec, err
	}
	rec = GatewayKey{ID: id, Name: name, KeyHash: h, KeyPrefix: prefix, CreatedAt: now}
	return plain, rec, nil
}

func ValidateGatewayKey(db *sql.DB, plain string) (*GatewayKey, bool) {
	h := crypto.HashGatewayKey(plain)
	var k GatewayKey
	err := db.QueryRow(`SELECT id,name,key_hash,key_prefix,created_at,last_used_at FROM gateway_keys WHERE key_hash=?`, h).Scan(&k.ID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt)
	if err != nil {
		return nil, false
	}
	_, _ = db.Exec(`UPDATE gateway_keys SET last_used_at=? WHERE id=?`, database.Now(), k.ID)
	return &k, true
}

func ListGatewayKeys(db *sql.DB) ([]GatewayKey, error) {
	rows, err := db.Query(`SELECT id,name,key_hash,key_prefix,created_at,last_used_at FROM gateway_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayKey
	for rows.Next() {
		var k GatewayKey
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt); err == nil {
			out = append(out, k)
		}
	}
	return out, nil
}

// ---- admin sessions ----

func CreateSession(db *sql.DB, ttl time.Duration) (string, error) {
	tok, err := crypto.RandomKey(32)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(tok))
	now := time.Now().UTC()
	_, err = db.Exec(`INSERT INTO sessions(token_hash,created_at,expires_at) VALUES(?,?,?)`,
		hex.EncodeToString(sum[:]), now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano))
	if err != nil {
		return "", err
	}
	return tok, nil
}

func ValidateSession(db *sql.DB, tok string) bool {
	sum := sha256.Sum256([]byte(tok))
	var exp string
	if err := db.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash=?`, hex.EncodeToString(sum[:])).Scan(&exp); err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, exp)
	if err != nil || time.Now().UTC().After(t) {
		return false
	}
	return true
}

func GetSetting(db *sql.DB, key string) string {
	var v string
	_ = db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func SetSetting(db *sql.DB, key, value string) {
	_, _ = db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
}
