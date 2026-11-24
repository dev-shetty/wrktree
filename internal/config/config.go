// Package config reads the optional wrktree configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds the settings of wrktree.
type Config struct {
	Open    string
	Base    string
	Fetch   bool
	SizeTTL time.Duration
}

// Default returns the settings that apply when no file sets them.
func Default() Config {
	return Config{Open: "code -n {path}", Base: "", Fetch: true, SizeTTL: 24 * time.Hour}
}

// Path returns the location of the configuration file.
func Path() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "wrktree", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "wrktree", "config.toml")
}

type fileConfig struct {
	Open    *string `toml:"open"`
	Base    *string `toml:"base"`
	Fetch   *bool   `toml:"fetch"`
	SizeTTL *string `toml:"size_ttl"`
}

// Load reads file. A missing file gives Default(). A key that the file
// does not set keeps its default value.
func Load(file string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", file, err)
	}
	var fc fileConfig
	md, err := toml.Decode(string(data), &fc)
	if err != nil {
		return Default(), fmt.Errorf("config file %s: %w", file, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		names := make([]string, len(un))
		for i, k := range un {
			names[i] = k.String()
		}
		return Default(), fmt.Errorf("config file %s: unknown key %s", file, strings.Join(names, ", "))
	}
	if fc.Open != nil {
		cfg.Open = *fc.Open
	}
	if fc.Base != nil {
		cfg.Base = *fc.Base
	}
	if fc.Fetch != nil {
		cfg.Fetch = *fc.Fetch
	}
	if fc.SizeTTL != nil {
		d, err := time.ParseDuration(*fc.SizeTTL)
		if err != nil {
			return Default(), fmt.Errorf("config file %s: size_ttl: %w", file, err)
		}
		cfg.SizeTTL = d
	}
	return cfg, nil
}
