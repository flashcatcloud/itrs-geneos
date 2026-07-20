package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadOverlaysDefaultsAndEnvironmentKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	data := []byte(`flashduty:
  integration_key: file-key
  timeout: 2s
  retries: 1
  severity_map:
    WARNING: Critical
  labels:
    team: "${TEAM}"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, selected, err := Load(Options{
		ExplicitPath: path,
		Getenv: func(key string) string {
			if key == "FLASHDUTY_INTEGRATION_KEY" {
				return "env-key"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected != path {
		t.Fatalf("selected path %q, want %q", selected, path)
	}
	if cfg.FlashDuty.IntegrationKey != "env-key" {
		t.Fatalf("integration key = %q", cfg.FlashDuty.IntegrationKey)
	}
	if cfg.FlashDuty.Timeout != 2*time.Second || cfg.FlashDuty.Retries != 1 {
		t.Fatalf("unexpected retry config: timeout=%s retries=%d", cfg.FlashDuty.Timeout, cfg.FlashDuty.Retries)
	}
	if cfg.FlashDuty.SeverityMap["WARNING"] != "Critical" {
		t.Fatalf("custom severity mapping not applied")
	}
	if cfg.FlashDuty.SeverityMap["OK"] != "Ok" {
		t.Fatalf("default severity mapping was not retained")
	}
	if cfg.FlashDuty.Labels["team"] != "${TEAM}" {
		t.Fatalf("label template should remain unexpanded at config load")
	}
}

func TestLoadFindsWorkingDirectoryConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flashduty.yaml")
	if err := os.WriteFile(path, []byte("flashduty:\n  integration_key: local-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, selected, err := Load(Options{WorkingDir: dir, HomeDir: t.TempDir(), Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	if selected != path || cfg.FlashDuty.IntegrationKey != "local-key" {
		t.Fatalf("selected=%q key=%q", selected, cfg.FlashDuty.IntegrationKey)
	}
}

func TestLoadRejectsUnknownYAMLField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("flashduty:\n  integration_key: key\n  unexpected: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(Options{ExplicitPath: path, Getenv: func(string) string { return "" }}); err == nil {
		t.Fatal("expected unknown YAML field error")
	}
}

func TestLoadRequiresIntegrationKey(t *testing.T) {
	_, _, err := Load(Options{WorkingDir: t.TempDir(), HomeDir: t.TempDir(), Getenv: func(string) string { return "" }})
	if err == nil {
		t.Fatal("expected missing integration key error")
	}
}
