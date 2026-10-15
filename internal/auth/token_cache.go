package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/laurentL/salt-api-cli/internal/config"
)

const DefaultLeeway = 30 * time.Second

// CachedToken stores a Salt NetAPI authentication token and its metadata.
type CachedToken struct {
	Token   string  `json:"token"`
	Start   float64 `json:"start"`
	Expire  float64 `json:"expire"`
	User    string  `json:"user"`
	EAuth   string  `json:"eauth"`
	Profile string  `json:"profile"`
	URL     string  `json:"url"`
	Perms   []any   `json:"perms,omitempty"`
}

// IsValid returns true if the token is non-empty and not expiring within leeway.
func (t *CachedToken) IsValid(leeway time.Duration) bool {
	if t == nil || t.Token == "" {
		return false
	}
	if t.Expire <= 0 {
		return true
	}
	nowPlusLeeway := float64(time.Now().Add(leeway).UnixNano()) / 1e9
	return nowPlusLeeway < t.Expire
}

// ExpiresAt returns the token expiration time as time.Time.
func (t *CachedToken) ExpiresAt() time.Time {
	if t == nil || t.Expire <= 0 {
		return time.Time{}
	}
	sec := int64(t.Expire)
	nsec := int64((t.Expire - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}

// TokenStore manages reading and writing cached tokens on disk.
type TokenStore struct {
	path string
	mu   sync.Mutex
}

// NewTokenStore creates a TokenStore using the default cache directory or a custom file path.
func NewTokenStore(customPath string) (*TokenStore, error) {
	if customPath == "" {
		cacheDir, err := config.DefaultCacheDir()
		if err != nil {
			return nil, err
		}
		customPath = filepath.Join(cacheDir, "tokens.json")
	}
	return &TokenStore{path: customPath}, nil
}

// Path returns the file path of the token cache.
func (s *TokenStore) Path() string {
	return s.path
}

// CacheKey builds a deterministic key for a given profile, URL, username, and eauth.
func CacheKey(profile, url, username, eauth string) string {
	return fmt.Sprintf("%s|%s|%s|%s", profile, url, username, eauth)
}

// Get retrieves a valid token from the cache if available.
func (s *TokenStore) Get(profile, url, username, eauth string) (*CachedToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}
	key := CacheKey(profile, url, username, eauth)
	tok, ok := tokens[key]
	if !ok {
		return nil, nil
	}
	if !tok.IsValid(DefaultLeeway) {
		return nil, nil
	}
	return &tok, nil
}

// Put saves a token into the cache file atomically with 0600 permissions.
func (s *TokenStore) Put(tok CachedToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens, err := s.readAllLocked()
	if err != nil {
		tokens = make(map[string]CachedToken)
	}
	key := CacheKey(tok.Profile, tok.URL, tok.User, tok.EAuth)
	tokens[key] = tok
	return s.writeAllLocked(tokens)
}

// Delete removes a cached token for the specified key.
func (s *TokenStore) Delete(profile, url, username, eauth string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens, err := s.readAllLocked()
	if err != nil {
		return err
	}
	key := CacheKey(profile, url, username, eauth)
	delete(tokens, key)
	return s.writeAllLocked(tokens)
}

// ListAll returns all tokens stored on disk (including expired ones for inspection).
func (s *TokenStore) ListAll() (map[string]CachedToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readAllLocked()
}

func (s *TokenStore) readAllLocked() (map[string]CachedToken, error) {
	out := make(map[string]CachedToken)
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("failed to read token cache %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, nil
	}
	return out, nil
}

func (s *TokenStore) writeAllLocked(tokens map[string]CachedToken) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create token cache directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize tokens: %w", err)
	}
	tmpPath := fmt.Sprintf("%s.tmp.%d", s.path, os.Getpid())
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temporary token cache: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to replace token cache: %w", err)
	}
	return nil
}
