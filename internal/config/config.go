package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Defaults struct {
	OrderQuantity int    `json:"orderQuantity"`
	ReturnJars    int    `json:"returnJars"`
	Schedule      string `json:"schedule"`
	Timeslot      string `json:"timeslot"`
}

type GlobalConfig struct {
	CurrentProfile string   `json:"currentProfile"`
	Defaults       Defaults `json:"defaults"`
}

const (
	configFileName = "config.json"
	profilesDir    = "profiles"
)

func ConfigDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "bislericli"), nil
	case "windows":
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "bislericli"), nil
	default:
		// Linux and others: honor XDG_CONFIG_HOME
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "bislericli"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "bislericli"), nil
	}
}

func EnsureConfigDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, profilesDir), 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func ConfigFilePath() (string, error) {
	dir, err := EnsureConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func ProfilesDir() (string, error) {
	dir, err := EnsureConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, profilesDir), nil
}

func ProfilePath(name string) (string, error) {
	if name == "" {
		return "", errors.New("profile name required")
	}
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s.json", name)), nil
}

func validateProfileName(name string) error {
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return errors.New("invalid profile name")
	}
	if filepath.Base(name) != name {
		return errors.New("invalid profile name")
	}
	return nil
}

func DefaultConfig() GlobalConfig {
	return GlobalConfig{
		CurrentProfile: "default",
		Defaults: Defaults{
			OrderQuantity: 2,
			ReturnJars:    2,
			Schedule:      "twice-weekly",
			Timeslot:      "08:00 AM - 02:00 PM",
		},
	}
}

func LoadGlobalConfig() (GlobalConfig, error) {
	path, err := ConfigFilePath()
	if err != nil {
		return GlobalConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			if err := SaveGlobalConfig(cfg); err != nil {
				return GlobalConfig{}, err
			}
			return cfg, nil
		}
		return GlobalConfig{}, err
	}
	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, err
	}
	if cfg.CurrentProfile == "" {
		cfg.CurrentProfile = "default"
	}
	if cfg.Defaults.OrderQuantity == 0 {
		cfg.Defaults.OrderQuantity = 2
	}
	if cfg.Defaults.ReturnJars == 0 {
		cfg.Defaults.ReturnJars = 2
	}
	if cfg.Defaults.Schedule == "" {
		cfg.Defaults.Schedule = "twice-weekly"
	}
	if cfg.Defaults.Timeslot == "" {
		cfg.Defaults.Timeslot = "08:00 AM - 02:00 PM"
	}
	return cfg, nil
}

func SaveGlobalConfig(cfg GlobalConfig) error {
	path, err := ConfigFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
