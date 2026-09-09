// Package database wraps SQLite access (pure-Go driver, WAL mode).
package database

import (
	"database/sql"
	"embed"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// SchemaSQL is exported for migrations tooling; actual file lives in /migrations too.
func schemaSQL() string {
	b, _ := schemaFS.ReadFile("schema.sql")
	if len(b) > 0 {
		return string(b)
	}
	return ""
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)
	if _, err := db.Exec(schemaSQL()); err != nil {
		// fallback: try /migrations/schema.sql relative path handled by caller
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO routing_config(id,strategy,max_attempts,sticky_sessions,sticky_minutes,context_handoff) VALUES(1,'auto',5,1,30,0)`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
