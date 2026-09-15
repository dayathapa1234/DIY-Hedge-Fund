package config

import "testing"

func TestLoadDefaultsValidate(t *testing.T) {
	t.Setenv("ADDR", "")
	t.Setenv("PROVIDER_PRIORITY", "")

	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected default config to validate: %v", err)
	}
	if cfg.ServiceName != "market-data-service" {
		t.Fatalf("unexpected service name %q", cfg.ServiceName)
	}
}

func TestValidateRejectsMissingProviderPriority(t *testing.T) {
	cfg := Load()
	cfg.ProviderPriority = nil

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected empty provider priority to fail")
	}
}

func TestValidateRejectsInvalidLogOutput(t *testing.T) {
	cfg := Load()
	cfg.LogOutput = "database"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid log output to fail")
	}
}
