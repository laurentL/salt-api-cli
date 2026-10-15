package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/term"
)

const (
	AppName           = "sapi"
	DefaultEAuth      = "pam"
	DefaultTimeoutStr = "60s"
	DefaultOutputFmt  = "pretty"
)

// Config represents the root TOML configuration file (~/.config/sapi/config.toml).
type Config struct {
	DefaultProfile string             `toml:"default_profile" json:"default_profile" yaml:"default_profile"`
	DefaultOutput  string             `toml:"default_output,omitempty" json:"default_output,omitempty" yaml:"default_output,omitempty"`
	Profiles       map[string]Profile `toml:"profiles" json:"profiles" yaml:"profiles"`
}

// Profile represents a single Salt Master connection configuration.
type Profile struct {
	URL         string `toml:"url" json:"url" yaml:"url"`
	Username    string `toml:"username" json:"username" yaml:"username"`
	Password    string `toml:"password,omitempty" json:"password,omitempty" yaml:"password,omitempty"`
	PasswordEnv string `toml:"password_env,omitempty" json:"password_env,omitempty" yaml:"password_env,omitempty"`
	PasswordCmd string `toml:"password_cmd,omitempty" json:"password_cmd,omitempty" yaml:"password_cmd,omitempty"`
	EAuth       string `toml:"eauth,omitempty" json:"eauth,omitempty" yaml:"eauth,omitempty"`
	Token       string `toml:"token,omitempty" json:"token,omitempty" yaml:"token,omitempty"`
	VerifySSL   *bool  `toml:"verify_ssl,omitempty" json:"verify_ssl,omitempty" yaml:"verify_ssl,omitempty"`
	CABundle    string `toml:"ca_bundle,omitempty" json:"ca_bundle,omitempty" yaml:"ca_bundle,omitempty"`
	Timeout     string `toml:"timeout,omitempty" json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// Overrides holds CLI flag values that take precedence over environment variables and config files.
type Overrides struct {
	ConfigPath string
	Profile    string
	URL        string
	Username   string
	Password   string
	EAuth      string
	Token      string
	Insecure   bool
	CABundle   string
	Timeout    time.Duration
	Output     string
}

// ResolvedProfile is the fully merged and validated configuration ready for saltclient.
type ResolvedProfile struct {
	Name        string
	URL         string
	Username    string
	Password    string
	PasswordEnv string
	PasswordCmd string
	EAuth       string
	Token       string
	VerifySSL   bool
	CABundle    string
	Timeout     time.Duration
	Output      string
}

// DefaultConfigPath returns ~/.config/sapi/config.toml (or SALT_API_CONFIG if set).
func DefaultConfigPath() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("SALT_API_CONFIG")); envPath != "" {
		return envPath, nil
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("cannot determine config directory: %w", err)
		}
		cfgDir = filepath.Join(home, ".config")
	}
	return filepath.Join(cfgDir, AppName, "config.toml"), nil
}

// DefaultCacheDir returns ~/.cache/sapi (or SALT_API_CACHE_DIR if set).
func DefaultCacheDir() (string, error) {
	if envDir := strings.TrimSpace(os.Getenv("SALT_API_CACHE_DIR")); envDir != "" {
		return envDir, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("cannot determine cache directory: %w", err)
		}
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, AppName), nil
}

// Load reads the TOML configuration file from path. If the file does not exist, an empty Config is returned.
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}

	cfg := &Config{
		DefaultProfile: "default",
		DefaultOutput:  DefaultOutputFmt,
		Profiles:       make(map[string]Profile),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TOML config %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	if cfg.DefaultOutput == "" {
		cfg.DefaultOutput = DefaultOutputFmt
	}
	return cfg, nil
}

// Save writes the Config struct to path with 0600 permissions.
func Save(path string, cfg *Config) error {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config to TOML: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}
	return nil
}

// Resolve merges config.toml, SALT_API_* environment variables, and CLI Overrides.
func Resolve(ov Overrides) (*ResolvedProfile, *Config, error) {
	cfg, err := Load(ov.ConfigPath)
	if err != nil {
		return nil, nil, err
	}

	profileName := firstNonEmpty(
		ov.Profile,
		os.Getenv("SALT_API_PROFILE"),
		cfg.DefaultProfile,
		"default",
	)

	var p Profile
	if existing, ok := cfg.Profiles[profileName]; ok {
		p = existing
	} else if ov.Profile != "" && len(cfg.Profiles) > 0 {
		return nil, cfg, fmt.Errorf("profile %q not found in config (available: %s)", profileName, strings.Join(ProfileNames(cfg), ", "))
	}

	verifySSL := true
	if p.VerifySSL != nil {
		verifySSL = *p.VerifySSL
	}
	if envInsecure := strings.TrimSpace(os.Getenv("SALT_API_INSECURE")); envInsecure != "" {
		if b, err := strconv.ParseBool(envInsecure); err == nil && b {
			verifySSL = false
		}
	}
	if envVerify := strings.TrimSpace(os.Getenv("SALT_API_VERIFY_SSL")); envVerify != "" {
		if b, err := strconv.ParseBool(envVerify); err == nil {
			verifySSL = b
		}
	}
	if ov.Insecure {
		verifySSL = false
	}

	timeout := 60 * time.Second
	timeoutStr := firstNonEmpty(os.Getenv("SALT_API_TIMEOUT"), p.Timeout, DefaultTimeoutStr)
	if d, err := time.ParseDuration(timeoutStr); err == nil && d > 0 {
		timeout = d
	} else if secs, err := strconv.Atoi(timeoutStr); err == nil && secs > 0 {
		timeout = time.Duration(secs) * time.Second
	}
	if ov.Timeout > 0 {
		timeout = ov.Timeout
	}

	rawURL := firstNonEmpty(ov.URL, os.Getenv("SALT_API_URL"), p.URL)
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")

	username := firstNonEmpty(
		ov.Username,
		os.Getenv("SALT_API_USER"),
		os.Getenv("SALT_API_USERNAME"),
		p.Username,
	)

	password := firstNonEmpty(
		ov.Password,
		os.Getenv("SALT_API_PASSWORD"),
		p.Password,
	)

	eauth := firstNonEmpty(
		ov.EAuth,
		os.Getenv("SALT_API_EAUTH"),
		p.EAuth,
		DefaultEAuth,
	)

	token := firstNonEmpty(
		ov.Token,
		os.Getenv("SALT_API_TOKEN"),
		p.Token,
	)

	caBundle := firstNonEmpty(
		ov.CABundle,
		os.Getenv("SALT_API_CA_BUNDLE"),
		p.CABundle,
	)

	outputFmt := firstNonEmpty(
		ov.Output,
		os.Getenv("SALT_API_OUTPUT"),
		cfg.DefaultOutput,
		DefaultOutputFmt,
	)

	resolved := &ResolvedProfile{
		Name:        profileName,
		URL:         rawURL,
		Username:    username,
		Password:    password,
		PasswordEnv: p.PasswordEnv,
		PasswordCmd: p.PasswordCmd,
		EAuth:       eauth,
		Token:       token,
		VerifySSL:   verifySSL,
		CABundle:    caBundle,
		Timeout:     timeout,
		Output:      outputFmt,
	}

	return resolved, cfg, nil
}

// Validate checks that URL is set on the resolved profile.
func (r *ResolvedProfile) Validate() error {
	if r.URL == "" {
		return errors.New("salt-api URL is not configured (set via --url, SALT_API_URL, or ~/.config/sapi/config.toml)")
	}
	return nil
}

// ResolvePassword obtains the password from explicit config/env, PasswordEnv, PasswordCmd, or interactive prompt.
func (r *ResolvedProfile) ResolvePassword(allowPrompt bool) (string, error) {
	if r.Password != "" {
		return r.Password, nil
	}
	if r.PasswordEnv != "" {
		if val := os.Getenv(r.PasswordEnv); val != "" {
			return val, nil
		}
		return "", fmt.Errorf("environment variable %q (configured in password_env for profile %q) is empty or not set", r.PasswordEnv, r.Name)
	}
	if r.PasswordCmd != "" {
		cmd := exec.Command("sh", "-c", r.PasswordCmd)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("password_cmd failed for profile %q: %w (%s)", r.Name, err, strings.TrimSpace(stderr.String()))
		}
		return strings.TrimRight(stdout.String(), "\r\n"), nil
	}
	if allowPrompt && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "Password for %s@%s (%s): ", r.Username, r.URL, r.Name)
		bytePw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("failed to read password from terminal: %w", err)
		}
		return string(bytePw), nil
	}
	return "", fmt.Errorf("no password available for profile %q (set SALT_API_PASSWORD, password_env, password_cmd, or --password)", r.Name)
}

// ProfileNames returns a sorted slice or list of profile names in Config.
func ProfileNames(cfg *Config) []string {
	if cfg == nil || len(cfg.Profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.Profiles))
	for k := range cfg.Profiles {
		names = append(names, k)
	}
	return names
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
