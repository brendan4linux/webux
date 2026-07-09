package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all runtime configuration for Webux.
type Config struct {
	ListenAddr  string      `yaml:"listen_addr"`
	DataDir     string      `yaml:"data_dir"`
	TLSCertFile string      `yaml:"tls_cert_file"`
	TLSKeyFile  string      `yaml:"tls_key_file"`
	LogLevel    string      `yaml:"log_level"`
	LearnMode   bool        `yaml:"learn_mode"`
	Auth        AuthConfig  `yaml:"auth"`
	Files       FilesConfig `yaml:"files"`
}

// FilesConfig restricts the file manager to a subtree.
type FilesConfig struct {
	// Root is the top-level directory the file manager may access.
	// Defaults to /home. Set to / only if you understand the implications.
	Root string `yaml:"root"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	// BypassToken — when set, requests with X-Webux-Token: <value> header
	// skip authentication entirely. Use a long random string (32+ chars).
	// Example: openssl rand -hex 32
	BypassToken string `yaml:"bypass_token"`

	// JWTSecret — signing key for session tokens. Auto-generated
	// and stored in the DB if left empty here.
	JWTSecret string `yaml:"jwt_secret"`

	// Disabled — set to true to turn off all authentication (development only).
	Disabled bool `yaml:"disabled"`

	// AllowedUsers — if non-empty, only these Unix usernames may log in.
	// Any PAM/shadow-authenticated account NOT in this list is rejected
	// even with correct credentials. Recommended for multi-user hosts.
	// Example: ["bdubs", "ops-team"]
	AllowedUsers []string `yaml:"allowed_users"`
}

// Load reads config from a YAML file, then overlays environment variables.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Environment overrides
	if v := os.Getenv("WEBUX_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("WEBUX_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("WEBUX_BYPASS_TOKEN"); v != "" {
		cfg.Auth.BypassToken = v
	}
	if v := os.Getenv("WEBUX_AUTH_DISABLED"); v == "true" || v == "1" {
		cfg.Auth.Disabled = true
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		ListenAddr: ":8989",
		DataDir:    "/var/lib/webux",
		LogLevel:   "info",
		LearnMode:  true,
		Files:      FilesConfig{Root: "/home"},
	}
}
