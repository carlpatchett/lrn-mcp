package config

import (
	"os"
	"path/filepath"
)

// Config represents runtime configuration for learning-mcp
type Config struct {
	DBPath       string   `json:"db_path"`
	ConfigDir    string   `json:"config_dir"`
	DefaultTools []string `json:"default_tools"`
	AutoLearn    bool     `json:"auto_learn"`
}

// Load loads default configuration, reading environment overrides if present
func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	configDir := os.Getenv("LRN_CONFIG_DIR")
	if configDir == "" {
		configDir = os.Getenv("LEARNING_CONFIG_DIR")
	}
	if configDir == "" {
		// Check if legacy config exists
		legacyDir := filepath.Join(homeDir, ".config", "learning-mcp")
		if _, err := os.Stat(legacyDir); err == nil {
			configDir = legacyDir
		} else {
			configDir = filepath.Join(homeDir, ".config", "lrn")
		}
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, err
	}

	dbPath := os.Getenv("LRN_DB_PATH")
	if dbPath == "" {
		dbPath = os.Getenv("LEARNING_DB_PATH")
	}
	if dbPath == "" {
		dbPath = filepath.Join(configDir, "learning.db")
	}

	return &Config{
		DBPath:    dbPath,
		ConfigDir: configDir,
		DefaultTools: []string{
			"kubectl",
			"git",
			"jj",
			"docker",
			"bash",
			"terraform",
		},
		AutoLearn: true,
	}, nil
}
