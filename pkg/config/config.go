package config

import "time"

const (
	DefaultMaxMessages  = 16
	DefaultSessionTTL   = 45 * time.Minute
	DefaultSweep        = 1 * time.Minute
	DefaultMinUserChars = 4
	DefaultDedupeWindow = 1500 * time.Millisecond
	DefaultMergeWindow  = 3 * time.Second
	DefaultRole         = "user"
	// Keep this in sync with Engram.yaml defaults.
	DefaultIgnorePattern = `(?i)^(mhm+|hmm+|uh+|um+|ok(?:ay)?|mm+|huh)[.!? ]*$`
)

// Config controls in-memory conversation buffering behavior.
type Config struct {
	MaxMessages   int           `json:"maxMessages" mapstructure:"maxMessages"`
	SessionTTL    time.Duration `json:"sessionTTL" mapstructure:"sessionTTL"`
	Sweep         time.Duration `json:"sweepInterval" mapstructure:"sweepInterval"`
	MinUserChars  int           `json:"minUserChars" mapstructure:"minUserChars"`
	IgnorePattern string        `json:"ignorePattern" mapstructure:"ignorePattern"`
	DedupeWindow  time.Duration `json:"dedupeWindow" mapstructure:"dedupeWindow"`
	MergeWindow   time.Duration `json:"mergeWindowMs" mapstructure:"mergeWindowMs"`
	DefaultRole   string        `json:"defaultRole" mapstructure:"defaultRole"`
}

func Normalize(cfg Config) Config {
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = DefaultMaxMessages
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}
	if cfg.Sweep <= 0 {
		cfg.Sweep = DefaultSweep
	}
	if cfg.MinUserChars <= 0 {
		cfg.MinUserChars = DefaultMinUserChars
	}
	if cfg.IgnorePattern == "" {
		cfg.IgnorePattern = DefaultIgnorePattern
	}
	if cfg.DedupeWindow < 0 {
		cfg.DedupeWindow = 0
	} else if cfg.DedupeWindow == 0 {
		cfg.DedupeWindow = DefaultDedupeWindow
	}
	if cfg.MergeWindow < 0 {
		cfg.MergeWindow = 0
	} else if cfg.MergeWindow == 0 {
		cfg.MergeWindow = DefaultMergeWindow
	}
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = DefaultRole
	}
	return cfg
}
