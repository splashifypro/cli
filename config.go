package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the on-disk state for the splashify CLI, stored at
// ~/.splashify/config.json (0600). It holds the backend URL, the user's
// `oc_live_` access token, and the resolved path to the splashify-mcp binary.
type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
	MCPPath string `json:"mcp_path,omitempty"`
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".splashify"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// loadConfig reads the CLI config. A missing file yields a zero Config and no
// error — callers check for an empty token themselves.
func loadConfig() (Config, error) {
	var cfg Config
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// saveConfig writes the CLI config with 0600 permissions (it holds a secret).
func saveConfig(cfg Config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// requireConfig loads the config and fails with a friendly message if the CLI
// has not been connected yet.
func requireConfig() (Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return cfg, err
	}
	if cfg.Token == "" || cfg.BaseURL == "" {
		return cfg, fmt.Errorf("not connected — run `splashify connect` first")
	}
	return cfg, nil
}
