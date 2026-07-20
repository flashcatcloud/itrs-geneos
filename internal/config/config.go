package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

const DefaultEndpoint = "https://api.flashcat.cloud/event/push/alert/standard"

type Config struct {
	FlashDuty FlashDutyConfig
}

type FlashDutyConfig struct {
	Endpoint       string
	IntegrationKey string
	Timeout        time.Duration
	Retries        int
	AlertKeyPrefix string
	SeverityMap    map[string]string
	Title          string
	Description    string
	Labels         map[string]string
}

type Options struct {
	ExplicitPath string
	WorkingDir   string
	HomeDir      string
	Getenv       func(string) string
}

type fileConfig struct {
	FlashDuty *fileFlashDuty `yaml:"flashduty"`
}

type fileFlashDuty struct {
	Endpoint       *string           `yaml:"endpoint"`
	IntegrationKey *string           `yaml:"integration_key"`
	Timeout        *string           `yaml:"timeout"`
	Retries        *int              `yaml:"retries"`
	AlertKey       *fileAlertKey     `yaml:"alert_key"`
	SeverityMap    map[string]string `yaml:"severity_map"`
	Title          *string           `yaml:"title"`
	Description    *string           `yaml:"description"`
	Labels         map[string]string `yaml:"labels"`
}

type fileAlertKey struct {
	Prefix *string `yaml:"prefix"`
}

func Defaults() Config {
	return Config{FlashDuty: FlashDutyConfig{
		Endpoint:       DefaultEndpoint,
		Timeout:        10 * time.Second,
		Retries:        3,
		AlertKeyPrefix: "geneos:v1:",
		SeverityMap: map[string]string{
			"0":         "Info",
			"UNDEFINED": "Info",
			"INFO":      "Info",
			"1":         "Ok",
			"OK":        "Ok",
			"2":         "Warning",
			"WARNING":   "Warning",
			"3":         "Critical",
			"CRITICAL":  "Critical",
		},
		Title:       "${_RULE} Triggered",
		Description: "entity=${_MANAGED_ENTITY}, value=${_VALUE}, path=${_VARIABLEPATH}",
		Labels:      map[string]string{},
	}}
}

func Load(opts Options) (Config, string, error) {
	cfg := Defaults()
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	path, explicit, err := discover(opts)
	if err != nil {
		return Config{}, "", err
	}
	if path != "" {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if explicit {
				return Config{}, "", fmt.Errorf("read config %q: %w", path, readErr)
			}
			return Config{}, "", readErr
		}
		if err := overlay(&cfg, data); err != nil {
			return Config{}, "", fmt.Errorf("parse config %q: %w", path, err)
		}
	}

	if key := strings.TrimSpace(getenv("FLASHDUTY_INTEGRATION_KEY")); key != "" {
		cfg.FlashDuty.IntegrationKey = key
	}
	if err := Validate(cfg); err != nil {
		return Config{}, path, err
	}
	return cfg, path, nil
}

func Validate(cfg Config) error {
	fd := cfg.FlashDuty
	if strings.TrimSpace(fd.IntegrationKey) == "" {
		return errors.New("FlashDuty integration key is required")
	}
	u, err := url.Parse(fd.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid FlashDuty endpoint %q", fd.Endpoint)
	}
	if fd.Timeout <= 0 {
		return errors.New("FlashDuty timeout must be greater than zero")
	}
	if fd.Retries < 0 {
		return errors.New("FlashDuty retries cannot be negative")
	}
	if strings.TrimSpace(fd.AlertKeyPrefix) == "" {
		return errors.New("alert key prefix is required")
	}
	return nil
}

func overlay(cfg *Config, data []byte) error {
	var parsed fileConfig
	if err := yaml.UnmarshalStrict(data, &parsed); err != nil {
		return err
	}
	if parsed.FlashDuty == nil {
		return nil
	}
	p := parsed.FlashDuty
	f := &cfg.FlashDuty
	if p.Endpoint != nil {
		f.Endpoint = *p.Endpoint
	}
	if p.IntegrationKey != nil {
		f.IntegrationKey = *p.IntegrationKey
	}
	if p.Timeout != nil {
		d, err := time.ParseDuration(*p.Timeout)
		if err != nil {
			return fmt.Errorf("invalid flashduty.timeout: %w", err)
		}
		f.Timeout = d
	}
	if p.Retries != nil {
		f.Retries = *p.Retries
	}
	if p.AlertKey != nil && p.AlertKey.Prefix != nil {
		f.AlertKeyPrefix = *p.AlertKey.Prefix
	}
	for k, v := range p.SeverityMap {
		f.SeverityMap[strings.ToUpper(k)] = v
	}
	if p.Title != nil {
		f.Title = *p.Title
	}
	if p.Description != nil {
		f.Description = *p.Description
	}
	for k, v := range p.Labels {
		f.Labels[k] = v
	}
	return nil
}

func discover(opts Options) (string, bool, error) {
	if opts.ExplicitPath != "" {
		return opts.ExplicitPath, true, nil
	}
	wd := opts.WorkingDir
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return "", false, fmt.Errorf("get working directory: %w", err)
		}
	}
	home := opts.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	candidates := []string{filepath.Join(wd, "flashduty.yaml")}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".config", "geneos", "flashduty.yaml"))
	}
	candidates = append(candidates, "/etc/geneos/flashduty.yaml")
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, false, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", false, fmt.Errorf("inspect config %q: %w", candidate, err)
		}
	}
	return "", false, nil
}
