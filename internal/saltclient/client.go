package saltclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/laurentL/salt-api-cli/internal/auth"
	"github.com/laurentL/salt-api-cli/internal/config"
)

// Client is a high-level Salt NetAPI HTTP client with automatic token caching and renewal.
type Client struct {
	profile     *config.ResolvedProfile
	httpClient  *http.Client
	streamHTTP  *http.Client // HTTP client without global timeout for SSE /events
	tokenStore  *auth.TokenStore
	AllowPrompt bool
}

// New creates a new Salt NetAPI Client from a ResolvedProfile and optional TokenStore.
func New(profile *config.ResolvedProfile, store *auth.TokenStore, allowPrompt bool) (*Client, error) {
	if profile == nil {
		return nil, errors.New("resolved profile cannot be nil")
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: !profile.VerifySSL, //nolint:gosec
	}

	if profile.CABundle != "" {
		pemData, err := os.ReadFile(profile.CABundle)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA bundle %s: %w", profile.CABundle, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("failed to parse certificates from CA bundle %s", profile.CABundle)
		}
		tlsCfg.RootCAs = pool
	}

	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
	}

	return &Client{
		profile: profile,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   profile.Timeout,
		},
		streamHTTP: &http.Client{
			Transport: transport,
			Timeout:   0, // No global timeout for SSE streams; controlled via context
		},
		tokenStore:  store,
		AllowPrompt: allowPrompt,
	}, nil
}

// Profile returns the underlying ResolvedProfile.
func (c *Client) Profile() *config.ResolvedProfile {
	return c.profile
}

// Login authenticates against POST /login and caches the resulting X-Auth-Token.
func (c *Client) Login(ctx context.Context) (*auth.CachedToken, error) {
	if c.profile.Username == "" {
		return nil, fmt.Errorf("username is required to login on profile %q (set --user, SALT_API_USER, or config.toml)", c.profile.Name)
	}
	password, err := c.profile.ResolvePassword(c.AllowPrompt)
	if err != nil {
		return nil, err
	}

	reqBody := LoginRequest{
		Username: c.profile.Username,
		Password: password,
		EAuth:    c.profile.EAuth,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := c.profile.URL + "/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /login failed: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read /login response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("authentication failed on %s (HTTP %d): %s", c.profile.URL, resp.StatusCode, strings.TrimSpace(string(respData)))
	}

	var loginResp LoginResponse
	if err := json.Unmarshal(respData, &loginResp); err != nil {
		return nil, fmt.Errorf("failed to parse /login JSON response: %w", err)
	}
	if len(loginResp.Return) == 0 || loginResp.Return[0].Token == "" {
		return nil, errors.New("/login returned an empty token")
	}

	info := loginResp.Return[0]
	cached := auth.CachedToken{
		Token:   info.Token,
		Start:   info.Start,
		Expire:  info.Expire,
		User:    firstNonEmpty(info.User, c.profile.Username),
		EAuth:   firstNonEmpty(info.EAuth, c.profile.EAuth),
		Profile: c.profile.Name,
		URL:     c.profile.URL,
		Perms:   info.Perms,
	}

	if c.tokenStore != nil {
		_ = c.tokenStore.Put(cached)
	}
	return &cached, nil
}

// Logout invalidates the current session via POST /logout and removes the cached token.
func (c *Client) Logout(ctx context.Context) error {
	var tok string
	if c.tokenStore != nil {
		if cached, _ := c.tokenStore.Get(c.profile.Name, c.profile.URL, c.profile.Username, c.profile.EAuth); cached != nil {
			tok = cached.Token
		}
		_ = c.tokenStore.Delete(c.profile.Name, c.profile.URL, c.profile.Username, c.profile.EAuth)
	}
	if tok == "" {
		tok = c.profile.Token
	}
	if tok == "" {
		return nil
	}

	url := c.profile.URL + "/logout"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-Token", tok)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /logout failed: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// EnsureToken returns a valid X-Auth-Token, logging in automatically if needed.
func (c *Client) EnsureToken(ctx context.Context, forceRefresh bool) (string, error) {
	if !forceRefresh && c.profile.Token != "" {
		return c.profile.Token, nil
	}
	if !forceRefresh && c.tokenStore != nil {
		if cached, err := c.tokenStore.Get(c.profile.Name, c.profile.URL, c.profile.Username, c.profile.EAuth); err == nil && cached != nil {
			return cached.Token, nil
		}
	}
	cached, err := c.Login(ctx)
	if err != nil {
		return "", err
	}
	return cached.Token, nil
}

// doJSON performs an authenticated JSON request against salt-api and automatically retries once on 401.
func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) ([]byte, error) {
	var bodyBytes []byte
	var err error
	if payload != nil {
		bodyBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request payload: %w", err)
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		forceRefresh := attempt > 0
		token, err := c.EnsureToken(ctx, forceRefresh)
		if err != nil {
			return nil, err
		}

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		fullURL := c.profile.URL + "/" + strings.TrimLeft(path, "/")
		if path == "" || path == "/" {
			fullURL = c.profile.URL + "/"
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("X-Auth-Token", token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s %s failed: %w", method, path, err)
		}

		respBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("failed to read response body from %s: %w", path, readErr)
		}

		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 && c.profile.Token == "" {
			if c.tokenStore != nil {
				_ = c.tokenStore.Delete(c.profile.Name, c.profile.URL, c.profile.Username, c.profile.EAuth)
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("salt-api error %s %s (HTTP %d): %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBytes)))
		}

		if out != nil && len(respBytes) > 0 {
			if err := json.Unmarshal(respBytes, out); err != nil {
				return respBytes, fmt.Errorf("failed to decode JSON response from %s: %w", path, err)
			}
		}
		return respBytes, nil
	}

	return nil, errors.New("authentication failed after token refresh retry")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
