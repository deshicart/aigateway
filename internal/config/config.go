// Package config loads OpenBridge configuration from env, file and flags.
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Host        string
	Port        int
	DataDir     string
	MasterKey   string
	LogLevel    string
	LowResource bool

	MaxBodyBytes       int64
	MaxConcurrent      int
	RequestTimeoutSec  int
	UpstreamTimeoutSec int
	MaxStreamSec       int
	MaxAttempts        int

	AdminUser         string
	AdminPassHashFile string
}

func DefaultDataDir() string {
	if v := os.Getenv("OPENBRIDGE_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "./.openbridge"
	}
	return filepath.Join(home, ".openbridge")
}

func Load() *Config {
	c := &Config{
		Host:               envOr("OPENBRIDGE_HOST", "127.0.0.1"),
		Port:               envInt("OPENBRIDGE_PORT", 8787),
		DataDir:            DefaultDataDir(),
		MasterKey:          os.Getenv("OPENBRIDGE_MASTER_KEY"),
		LogLevel:           envOr("OPENBRIDGE_LOG_LEVEL", "INFO"),
		LowResource:        envBool("OPENBRIDGE_LOW_RESOURCE", false),
		MaxBodyBytes:       int64(envInt("OPENBRIDGE_MAX_BODY", 4*1024*1024)),
		MaxConcurrent:      envInt("OPENBRIDGE_MAX_CONCURRENT", 64),
		RequestTimeoutSec:  envInt("OPENBRIDGE_REQUEST_TIMEOUT", 120),
		UpstreamTimeoutSec: envInt("OPENBRIDGE_UPSTREAM_TIMEOUT", 90),
		MaxStreamSec:       envInt("OPENBRIDGE_MAX_STREAM", 300),
		MaxAttempts:        envInt("OPENBRIDGE_MAX_ATTEMPTS", 5),
		AdminUser:          envOr("OPENBRIDGE_ADMIN_USER", "admin"),
	}
	if c.LowResource {
		if c.MaxConcurrent > 16 {
			c.MaxConcurrent = 16
		}
		if c.MaxAttempts > 3 {
			c.MaxAttempts = 3
		}
	}
	return c
}

func (c *Config) Addr() string { return c.Host + ":" + strconv.Itoa(c.Port) }

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}

func envBool(k string, d bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return d
	}
	return b
}
