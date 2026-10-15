package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAndResolve(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")

	verifyFalse := false
	initialCfg := &Config{
		DefaultProfile: "prod",
		DefaultOutput:  "yaml",
		Profiles: map[string]Profile{
			"prod": {
				URL:         "https://salt-prod.example.com:8000/",
				Username:    "admin",
				PasswordEnv: "TEST_SALT_PROD_PW",
				EAuth:       "ldap",
				Timeout:     "45s",
			},
			"dev": {
				URL:         "https://salt-dev.example.com:8000",
				Username:    "devuser",
				PasswordCmd: "printf 'secret-from-cmd\\n'",
				EAuth:       "pam",
				VerifySSL:   &verifyFalse,
			},
		},
	}

	if err := Save(cfgPath, initialCfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	t.Setenv("TEST_SALT_PROD_PW", "super-secret-prod")

	resolved, _, err := Resolve(Overrides{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("Resolve default failed: %v", err)
	}
	if resolved.Name != "prod" {
		t.Errorf("expected profile prod, got %s", resolved.Name)
	}
	if resolved.URL != "https://salt-prod.example.com:8000" {
		t.Errorf("unexpected URL: %s", resolved.URL)
	}
	if resolved.EAuth != "ldap" {
		t.Errorf("expected eauth ldap, got %s", resolved.EAuth)
	}
	if !resolved.VerifySSL {
		t.Errorf("expected VerifySSL true by default")
	}
	if resolved.Timeout != 45*time.Second {
		t.Errorf("expected timeout 45s, got %v", resolved.Timeout)
	}
	if resolved.Output != "yaml" {
		t.Errorf("expected output yaml, got %s", resolved.Output)
	}

	pw, err := resolved.ResolvePassword(false)
	if err != nil {
		t.Fatalf("ResolvePassword failed: %v", err)
	}
	if pw != "super-secret-prod" {
		t.Errorf("expected super-secret-prod, got %s", pw)
	}

	// Test dev profile with password_cmd and verify_ssl=false
	resolvedDev, _, err := Resolve(Overrides{ConfigPath: cfgPath, Profile: "dev"})
	if err != nil {
		t.Fatalf("Resolve dev failed: %v", err)
	}
	if resolvedDev.VerifySSL {
		t.Errorf("expected VerifySSL false for dev")
	}
	devPw, err := resolvedDev.ResolvePassword(false)
	if err != nil {
		t.Fatalf("ResolvePassword dev failed: %v", err)
	}
	if devPw != "secret-from-cmd" {
		t.Errorf("expected secret-from-cmd, got %q", devPw)
	}

	// Test environment overrides
	os.Setenv("SALT_API_URL", "https://override.example.com:8000")
	defer os.Unsetenv("SALT_API_URL")

	resolvedEnv, _, err := Resolve(Overrides{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("Resolve with env failed: %v", err)
	}
	if resolvedEnv.URL != "https://override.example.com:8000" {
		t.Errorf("expected env override URL, got %s", resolvedEnv.URL)
	}
}
