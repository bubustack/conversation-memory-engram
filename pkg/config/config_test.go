package config_test

import (
	"testing"
	"time"

	"github.com/bubustack/conversation-memory-engram/pkg/config"
)

func TestNormalize_MergeWindowDisabled(t *testing.T) {
	// mergeWindowMs: -1 should disable merging (window = 0), not fall through to default.
	cfg := config.Config{MergeWindow: -1 * time.Millisecond}
	got := config.Normalize(cfg)
	if got.MergeWindow != 0 {
		t.Errorf("MergeWindow -1 should normalize to 0 (disabled), got %v", got.MergeWindow)
	}
}

func TestNormalize_MergeWindowDefault(t *testing.T) {
	// Unset (zero value) should fall back to DefaultMergeWindow.
	cfg := config.Config{}
	got := config.Normalize(cfg)
	if got.MergeWindow != config.DefaultMergeWindow {
		t.Errorf("MergeWindow unset should normalize to DefaultMergeWindow %v, got %v",
			config.DefaultMergeWindow, got.MergeWindow)
	}
}

func TestNormalize_MergeWindowExplicit(t *testing.T) {
	// Explicitly set positive value should be preserved.
	cfg := config.Config{MergeWindow: 5 * time.Second}
	got := config.Normalize(cfg)
	if got.MergeWindow != 5*time.Second {
		t.Errorf("MergeWindow explicit 5s should be preserved, got %v", got.MergeWindow)
	}
}

func TestNormalize_DedupeWindowDisabled(t *testing.T) {
	// dedupeWindowMs: -1 should disable deduplication (window = 0), not fall through to default.
	cfg := config.Config{DedupeWindow: -1 * time.Millisecond}
	got := config.Normalize(cfg)
	if got.DedupeWindow != 0 {
		t.Errorf("DedupeWindow -1 should normalize to 0 (disabled), got %v", got.DedupeWindow)
	}
}

func TestNormalize_DedupeWindowDefault(t *testing.T) {
	cfg := config.Config{}
	got := config.Normalize(cfg)
	if got.DedupeWindow != config.DefaultDedupeWindow {
		t.Errorf("DedupeWindow unset should normalize to DefaultDedupeWindow %v, got %v",
			config.DefaultDedupeWindow, got.DedupeWindow)
	}
}
