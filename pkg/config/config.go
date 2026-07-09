// Package config loads GoForge app configuration with the precedence:
// built-in defaults < config file (YAML) < environment (FORGE_*) < flags.
// Runtime-tunable settings (mail, auth providers, ...) live in the database
// settings store instead — this package only covers boot-time config.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// AppName is used in emails, admin UI and MCP server info.
	AppName string `yaml:"appName"`
	// AppURL is the public base URL (used in email links, OAuth redirects).
	AppURL string `yaml:"appUrl"`
	// DataDir holds the SQLite db, uploaded files, backups and generated keys.
	DataDir string `yaml:"dataDir"`
	// Secret is the master secret for JWTs and settings encryption.
	// If empty, a random one is generated and persisted to DataDir/.secret.
	Secret string `yaml:"secret"`
	// Dev enables verbose logging and disables some hardening (secure cookies).
	Dev bool `yaml:"dev"`

	HTTP HTTPConfig `yaml:"http"`
	DB   DBConfig   `yaml:"db"`
	Log  LogConfig  `yaml:"log"`
}

type HTTPConfig struct {
	// Addr like ":8090".
	Addr string `yaml:"addr"`
	// HTTPSDomains enables Let's Encrypt auto-TLS for the given domains.
	HTTPSDomains []string `yaml:"httpsDomains"`
	// CORSOrigins allowed for browser clients. Default "*".
	CORSOrigins []string `yaml:"corsOrigins"`
	// TrustProxy trusts X-Forwarded-* headers (behind a reverse proxy).
	TrustProxy bool `yaml:"trustProxy"`
}

type DBConfig struct {
	// Driver: sqlite | postgres | mysql
	Driver string `yaml:"driver"`
	// DSN: driver-specific. For sqlite it may be empty (uses DataDir/data.db).
	DSN string `yaml:"dsn"`
	// MaxOpenConns / MaxIdleConns tune the pool (0 = driver defaults).
	MaxOpenConns int `yaml:"maxOpenConns"`
	MaxIdleConns int `yaml:"maxIdleConns"`
}

type LogConfig struct {
	// Level: debug | info | warn | error
	Level string `yaml:"level"`
	// JSON switches to structured JSON logs (recommended in production).
	JSON bool `yaml:"json"`
	// Requests persists request logs to the database (viewable in admin).
	Requests bool `yaml:"requests"`
	// RetentionDays for persisted logs. 0 disables cleanup.
	RetentionDays int `yaml:"retentionDays"`
}

// Default returns the baseline configuration.
func Default() *Config {
	return &Config{
		AppName: "GoForge App",
		AppURL:  "http://localhost:8090",
		DataDir: "forge_data",
		HTTP: HTTPConfig{
			Addr:        ":8090",
			CORSOrigins: []string{"*"},
		},
		DB: DBConfig{Driver: "sqlite"},
		Log: LogConfig{
			Level:         "info",
			Requests:      true,
			RetentionDays: 7,
		},
	}
}

// Load builds the effective config: defaults, then optional YAML file,
// then FORGE_* environment overrides. path may be empty.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("config: read %s: %w", path, err)
			}
		} else if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	applyEnv(cfg)
	return cfg, nil
}

// applyEnv maps FORGE_* variables onto the config.
func applyEnv(c *Config) {
	set := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	setBool := func(key string, dst *bool) {
		if v, ok := os.LookupEnv(key); ok {
			if b, err := strconv.ParseBool(v); err == nil {
				*dst = b
			}
		}
	}
	setList := func(key string, dst *[]string) {
		if v, ok := os.LookupEnv(key); ok {
			parts := strings.Split(v, ",")
			out := parts[:0]
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			*dst = out
		}
	}

	set("FORGE_APP_NAME", &c.AppName)
	set("FORGE_APP_URL", &c.AppURL)
	set("FORGE_DATA_DIR", &c.DataDir)
	set("FORGE_SECRET", &c.Secret)
	setBool("FORGE_DEV", &c.Dev)
	set("FORGE_HTTP_ADDR", &c.HTTP.Addr)
	setList("FORGE_HTTPS_DOMAINS", &c.HTTP.HTTPSDomains)
	setList("FORGE_CORS_ORIGINS", &c.HTTP.CORSOrigins)
	setBool("FORGE_TRUST_PROXY", &c.HTTP.TrustProxy)
	set("FORGE_DB_DRIVER", &c.DB.Driver)
	set("FORGE_DB_DSN", &c.DB.DSN)
	set("FORGE_LOG_LEVEL", &c.Log.Level)
	setBool("FORGE_LOG_JSON", &c.Log.JSON)
	setBool("FORGE_LOG_REQUESTS", &c.Log.Requests)
}

// EnsureSecret returns cfg.Secret, generating and persisting one under
// DataDir/.secret on first run when unset.
func (c *Config) EnsureSecret(random func(int) string) (string, error) {
	if c.Secret != "" {
		return c.Secret, nil
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(c.DataDir, ".secret")
	if raw, err := os.ReadFile(p); err == nil && len(raw) >= 32 {
		c.Secret = strings.TrimSpace(string(raw))
		return c.Secret, nil
	}
	s := random(48)
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		return "", err
	}
	c.Secret = s
	return s, nil
}

// SQLiteDSN resolves the effective sqlite DSN.
func (c *Config) SQLiteDSN() string {
	if c.DB.DSN != "" {
		return c.DB.DSN
	}
	return filepath.Join(c.DataDir, "data.db")
}
