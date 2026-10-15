package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStore(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "tokens.json")

	store, err := NewTokenStore(cachePath)
	if err != nil {
		t.Fatalf("NewTokenStore failed: %v", err)
	}

	now := float64(time.Now().Unix())
	validTok := CachedToken{
		Token:   "tok-valid-123",
		Start:   now - 60,
		Expire:  now + 3600,
		User:    "admin",
		EAuth:   "pam",
		Profile: "prod",
		URL:     "https://salt-prod:8000",
		Perms:   []any{".*"},
	}

	if err := store.Put(validTok); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	got, err := store.Get("prod", "https://salt-prod:8000", "admin", "pam")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil || got.Token != "tok-valid-123" {
		t.Fatalf("expected tok-valid-123, got %+v", got)
	}

	// Expired token should return nil
	expiredTok := validTok
	expiredTok.Token = "tok-expired"
	expiredTok.Expire = now + 5 // within 30s leeway -> treated as expired
	if err := store.Put(expiredTok); err != nil {
		t.Fatalf("Put expired failed: %v", err)
	}

	gotExpired, err := store.Get("prod", "https://salt-prod:8000", "admin", "pam")
	if err != nil {
		t.Fatalf("Get expired failed: %v", err)
	}
	if gotExpired != nil {
		t.Errorf("expected nil for expiring token, got %+v", gotExpired)
	}

	// Delete token
	if err := store.Delete("prod", "https://salt-prod:8000", "admin", "pam"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	all, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty cache after delete, got %d", len(all))
	}
}
