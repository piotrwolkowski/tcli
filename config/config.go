package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ClientID string `json:"clientId"`
	TenantID string `json:"tenantId"`
}

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "tcli"), nil
}

func Load() (*Config, error) {
	cfg := &Config{
		ClientID: os.Getenv("TCLI_CLIENT_ID"),
		TenantID: os.Getenv("TCLI_TENANT_ID"),
	}

	dir, err := Dir()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, validate(cfg)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var fileCfg Config
	if err := json.Unmarshal(data, &fileCfg); err != nil {
		return nil, fmt.Errorf("parsing config.json: %w", err)
	}

	// Env vars take precedence over file values
	if cfg.ClientID == "" {
		cfg.ClientID = fileCfg.ClientID
	}
	if cfg.TenantID == "" {
		cfg.TenantID = fileCfg.TenantID
	}

	return cfg, validate(cfg)
}

func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0600)
}

func validate(cfg *Config) error {
	if cfg.ClientID == "" || cfg.TenantID == "" {
		return fmt.Errorf("clientId and tenantId are required — set TCLI_CLIENT_ID / TCLI_TENANT_ID env vars or run: tcli config")
	}
	return nil
}
