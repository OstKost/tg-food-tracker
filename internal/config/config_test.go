package config

import (
	"os"
	"testing"
)

func setEnv(t *testing.T, k, v string) {
	old, ok := os.LookupEnv(k)
	if err := os.Setenv(k, v); err != nil {
		t.Fatalf("failed to setenv: %v", err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(k, old)
		} else {
			_ = os.Unsetenv(k)
		}
	})
}

func unsetEnv(t *testing.T, k string) {
	old, ok := os.LookupEnv(k)
	_ = os.Unsetenv(k)
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(k, old)
		}
	})
}

func TestConfigValidation(t *testing.T) {
	setEnv(t, "TELEGRAM_BOT_TOKEN", "fake-token")
	setEnv(t, "ALLOWED_TELEGRAM_USER_ID", "1234567")
	setEnv(t, "GOOGLE_SHEETS_SPREADSHEET_ID", "spreadsheet-id-12345")
	setEnv(t, "GOOGLE_SERVICE_ACCOUNT_FILE", "/tmp/service-account.json")
	setEnv(t, "AI_PROVIDER", "openai")
	unsetEnv(t, "OPENAI_API_KEY")

	// Missing openai key
	_, err := Load("")
	if err == nil {
		t.Fatalf("expected error when OPENAI_API_KEY is missing for openai provider")
	}

	setEnv(t, "OPENAI_API_KEY", "sk-test")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AIProvider != "openai" || cfg.AllowedTelegramUserID != 1234567 {
		t.Fatalf("unexpected config values: %+v", cfg)
	}

	// Switch to gemini
	setEnv(t, "AI_PROVIDER", "gemini")
	unsetEnv(t, "GEMINI_API_KEY")
	_, err = Load("")
	if err == nil {
		t.Fatalf("expected error when GEMINI_API_KEY is missing for gemini provider")
	}

	setEnv(t, "GEMINI_API_KEY", "gemini-key-test")
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AIProvider != "gemini" {
		t.Fatalf("expected provider gemini, got %s", cfg.AIProvider)
	}
}
