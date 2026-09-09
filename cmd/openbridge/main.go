// Command openbridge is the standalone OpenBridge Gateway executable.
package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openbridge/gateway/internal/auth"
	"github.com/openbridge/gateway/internal/config"
	"github.com/openbridge/gateway/internal/crypto"
	"github.com/openbridge/gateway/internal/database"
	"github.com/openbridge/gateway/internal/gateway"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "start":
			runServer(filterArgs(os.Args[2:]))
			return
		case "status":
			cmdStatus()
			return
		case "providers":
			cmdProviders()
			return
		case "models":
			cmdModels()
			return
		case "key":
			cmdKey(os.Args[2:])
			return
		case "config":
			cmdConfig(os.Args[2:])
			return
		case "version", "--version", "-v":
			fmt.Println("openbridge " + gateway.Version)
			return
		case "help", "--help", "-h":
			usage()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
			usage()
			os.Exit(2)
		}
	}
	// default: start server
	runServer(nil)
}

func usage() {
	fmt.Println(`OpenBridge Gateway ` + gateway.Version + `
Usage:
  openbridge [start]            start the gateway (default)
  openbridge status             show local gateway status
  openbridge providers          list configured providers
  openbridge models             list catalog models
  openbridge key [create|list]  manage gateway API keys
  openbridge config             show effective configuration
  openbridge version            print version`)
}

func filterArgs(a []string) []string { return a }

func loadCore() (*config.Config, *sql.DB, []byte) {
	cfg := config.Load()
	for _, a := range os.Args {
		if strings.HasPrefix(a, "--port=") {
			fmt.Sscanf(strings.TrimPrefix(a, "--port="), "%d", &cfg.Port)
		}
		if strings.HasPrefix(a, "--host=") {
			cfg.Host = strings.TrimPrefix(a, "--host=")
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "cannot create data dir:", err)
		os.Exit(1)
	}
	master, err := crypto.LoadOrCreateMasterKey(cfg.MasterKey, cfg.DataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot load master key:", err)
		os.Exit(1)
	}
	db, err := database.Open(filepath.Join(cfg.DataDir, "openbridge.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open database:", err)
		os.Exit(1)
	}
	return cfg, db, master
}

func runServer(_ []string) {
	cfg, db, master := loadCore()
	defer db.Close()
	log := gateway.NewLogger(cfg.LogLevel)
	srv := gateway.New(db, cfg, master, log)
	// ensure at least one gateway key exists for first-run wizard display
	ensureFirstRun(db)
	httpSrv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      time.Duration(cfg.MaxStreamSec+30) * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if cfg.Host != "127.0.0.1" && cfg.Host != "localhost" && cfg.Host != "::1" {
		log.Warnf("EXPOSING OpenBridge on %s — others on this network may use your API quota. Never expose publicly without protection.", cfg.Addr())
	}
	log.Infof("OpenBridge %s listening on http://%s (dashboard + /v1 API)", gateway.Version, cfg.Addr())
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

func ensureFirstRun(db *sql.DB) {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM gateway_keys`).Scan(&n)
	if n == 0 {
		plain, _, err := auth.CreateGatewayKey(db, "Default")
		if err == nil {
			fmt.Printf("\n  First run: gateway API key generated:\n  %s\n  Copy it — shown in full only once (prefix visible in dashboard).\n\n", plain)
		}
	}
}

func baseURL(cfg *config.Config) string { return "http://" + cfg.Addr() }

func cmdStatus() {
	cfg := config.Load()
	resp, err := http.Get(baseURL(cfg) + "/health")
	if err != nil {
		fmt.Println("stopped (no response on", baseURL(cfg)+")")
		return
	}
	defer resp.Body.Close()
	fmt.Println("running —", baseURL(cfg), "version", gateway.Version)
}

func cmdProviders() {
	cfg, db, _ := loadCore()
	defer db.Close()
	rows, err := db.Query(`SELECT type,display_name,enabled FROM providers`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var t, n string
		var en int
		_ = rows.Scan(&t, &n, &en)
		st := "disabled"
		if en == 1 {
			st = "enabled"
		}
		fmt.Printf("%-12s %-30s %s\n", t, n, st)
		count++
	}
	if count == 0 {
		fmt.Println("no providers configured — open", baseURL(cfg), "and add one")
	}
}

func cmdModels() {
	cfg, db, _ := loadCore()
	defer db.Close()
	_ = cfg
	rows, err := db.Query(`SELECT model_id,enabled FROM model_overrides`)
	_ = rows
	_ = err
	// print static catalog (id + provider)
	for _, m := range catalogLines() {
		fmt.Println(m)
	}
}

func catalogLines() []string {
	// keep CLI dependency-free: query not needed; static hint
	return []string{
		"auto (virtual router)",
		"google/gemini-2.5-flash",
		"groq/llama-3.3-70b-versatile",
		"openrouter/auto",
		"cerebras/llama-3.3-70b",
		"mistral/mistral-large-latest",
		"nvidia/meta/llama-3.3-70b-instruct",
		"github/openai/gpt-4o",
		"cloudflare/@cf/meta/llama-3.3-70b-instruct-fp8-fast",
		"huggingface/meta-llama/Llama-3.3-70B-Instruct",
		"ollama/llama3.3",
	}
}

func cmdKey(args []string) {
	_, db, _ := loadCore()
	defer db.Close()
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "create":
		name := "CLI key"
		if len(args) > 1 {
			name = strings.Join(args[1:], " ")
		}
		plain, _, err := auth.CreateGatewayKey(db, name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(plain)
	default:
		keys, _ := auth.ListGatewayKeys(db)
		for _, k := range keys {
			fmt.Printf("%-20s %-14s created %s last-used %s\n", k.Name, k.KeyPrefix+"...", k.CreatedAt, k.LastUsedAt)
		}
		if len(keys) == 0 {
			fmt.Println("no gateway keys")
		}
	}
}

func cmdConfig(args []string) {
	cfg := config.Load()
	_ = args
	fmt.Printf("host=%s\nport=%d\ndata_dir=%s\nlog_level=%s\nlow_resource=%v\nmax_attempts=%d\n",
		cfg.Host, cfg.Port, cfg.DataDir, cfg.LogLevel, cfg.LowResource, cfg.MaxAttempts)
}
