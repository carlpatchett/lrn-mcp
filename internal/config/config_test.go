package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Load(t *testing.T) {
	t.Run("default paths", func(t *testing.T) {
		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error loading config: %v", err)
		}
		if cfg.DBPath == "" {
			t.Errorf("expected DBPath to be populated")
		}
		if len(cfg.DefaultTools) == 0 {
			t.Errorf("expected default tools to be populated")
		}
	})

	t.Run("environment override", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "config_test_*")
		if err != nil {
			t.Fatalf("failed creating temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		customDB := filepath.Join(tempDir, "custom.db")
		os.Setenv("LEARNING_DB_PATH", customDB)
		defer os.Unsetenv("LEARNING_DB_PATH")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error loading config: %v", err)
		}
		if cfg.DBPath != customDB {
			t.Errorf("expected DBPath %s, got %s", customDB, cfg.DBPath)
		}
	})
}
