package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Enabled       bool   `json:"enabled"`
	City          string `json:"city"`
	CandleMinutes int    `json:"candleMinutes"`
	LeadMinutes   int    `json:"leadMinutes"`
	Holidays      bool   `json:"holidays"`
	Mode          string `json:"mode"` // "shutdown" or "sleep"
	Force         bool   `json:"force"`
	SkipDate      string `json:"skipDate"`
	Accepted      bool   `json:"accepted"` // terms accepted; nothing runs before this
}

func defaultConfig() Config {
	return Config{Enabled: true, City: "ירושלים", CandleMinutes: 40, LeadMinutes: 30, Holidays: true, Mode: "shutdown"}
}

func (c Config) valid() bool {
	return c.CandleMinutes >= 0 && c.CandleMinutes <= 90 && c.LeadMinutes >= 5 && c.LeadMinutes <= 240 &&
		(c.Mode == "shutdown" || c.Mode == "sleep")
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "ShabbatMode", "settings.json")
}

func loadConfig() Config {
	cfg := defaultConfig()
	if b, err := os.ReadFile(configPath()); err == nil {
		var saved Config
		if json.Unmarshal(b, &saved) == nil && saved.valid() {
			cfg = saved
		}
	}
	return cfg
}

func saveConfig(c Config) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
