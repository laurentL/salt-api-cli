package cli

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/laurentL/salt-api-cli/internal/auth"
	"github.com/laurentL/salt-api-cli/internal/config"
	"github.com/spf13/cobra"
)

func newProfileCmd(state *AppState) *cobra.Command {
	profileCmd := &cobra.Command{
		Use:     "profile",
		Aliases: []string{"profiles"},
		Short:   "Manage Salt Master connection profiles in ~/.config/sapi/config.toml",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured Salt Master profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(state.Overrides.ConfigPath)
			if err != nil {
				return err
			}
			names := config.ProfileNames(cfg)
			sort.Strings(names)

			out := make(map[string]any, len(names))
			for _, name := range names {
				p := cfg.Profiles[name]
				verify := true
				if p.VerifySSL != nil {
					verify = *p.VerifySSL
				}
				entry := map[string]any{
					"url":        p.URL,
					"username":   p.Username,
					"eauth":      p.EAuth,
					"verify_ssl": verify,
					"default":    name == cfg.DefaultProfile,
				}
				if p.PasswordEnv != "" {
					entry["password_env"] = p.PasswordEnv
				}
				if p.PasswordCmd != "" {
					entry["password_cmd"] = p.PasswordCmd
				}
				out[name] = entry
			}
			return state.render(nil, out)
		},
	}

	showCmd := &cobra.Command{
		Use:   "show [profile_name]",
		Short: "Display the active/resolved configuration for a profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ov := state.Overrides
			if len(args) == 1 {
				ov.Profile = args[0]
			}
			resolved, _, err := config.Resolve(ov)
			if err != nil {
				return err
			}
			out := map[string]any{
				"profile":      resolved.Name,
				"url":          resolved.URL,
				"username":     resolved.Username,
				"eauth":        resolved.EAuth,
				"verify_ssl":   resolved.VerifySSL,
				"timeout":      resolved.Timeout.String(),
				"output":       resolved.Output,
				"has_password": resolved.Password != "" || resolved.PasswordEnv != "" || resolved.PasswordCmd != "",
				"has_token":    resolved.Token != "",
			}
			return state.render(resolved, out)
		},
	}

	setDefaultCmd := &cobra.Command{
		Use:   "set-default <profile_name>",
		Short: "Set the default profile in config.toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(state.Overrides.ConfigPath)
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q does not exist in config.toml", name)
			}
			cfg.DefaultProfile = name
			if err := config.Save(state.Overrides.ConfigPath, cfg); err != nil {
				return err
			}
			fmt.Printf("Default profile set to %q\n", name)
			return nil
		},
	}

	var (
		setURL         string
		setUser        string
		setPasswordEnv string
		setPasswordCmd string
		setEAuth       string
		setInsecure    bool
		setTimeout     string
		makeDefault    bool
	)
	setCmd := &cobra.Command{
		Use:   "set <profile_name>",
		Short: "Create or update a profile in ~/.config/sapi/config.toml",
		Example: `  sapi profile set prod --url https://salt-prod:8000 --user saltadmin --eauth pam --password-env SALT_PROD_PASS --default
  sapi profile set dev --url https://salt-dev:8000 --user admin --insecure`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(state.Overrides.ConfigPath)
			if err != nil {
				return err
			}
			name := args[0]
			p := cfg.Profiles[name]
			if setURL != "" {
				p.URL = setURL
			}
			if setUser != "" {
				p.Username = setUser
			}
			if setPasswordEnv != "" {
				p.PasswordEnv = setPasswordEnv
			}
			if setPasswordCmd != "" {
				p.PasswordCmd = setPasswordCmd
			}
			if setEAuth != "" {
				p.EAuth = setEAuth
			} else if p.EAuth == "" {
				p.EAuth = config.DefaultEAuth
			}
			if cmd.Flags().Changed("insecure") {
				v := !setInsecure
				p.VerifySSL = &v
			}
			if setTimeout != "" {
				p.Timeout = setTimeout
			}
			cfg.Profiles[name] = p
			if makeDefault || len(cfg.Profiles) == 1 {
				cfg.DefaultProfile = name
			}
			if err := config.Save(state.Overrides.ConfigPath, cfg); err != nil {
				return err
			}
			fmt.Printf("Profile %q saved successfully.\n", name)
			return nil
		},
	}
	setCmd.Flags().StringVar(&setURL, "url", "", "Salt API URL (e.g. https://salt-master:8000)")
	setCmd.Flags().StringVar(&setUser, "user", "", "Salt API username")
	setCmd.Flags().StringVar(&setPasswordEnv, "password-env", "", "Environment variable name storing the password")
	setCmd.Flags().StringVar(&setPasswordCmd, "password-cmd", "", "Shell command outputting the password")
	setCmd.Flags().StringVar(&setEAuth, "eauth", "", "Salt eauth backend (pam, ldap, file...)")
	setCmd.Flags().BoolVar(&setInsecure, "insecure", false, "Disable SSL certificate verification")
	setCmd.Flags().StringVar(&setTimeout, "timeout", "", "Request timeout (e.g. 60s)")
	setCmd.Flags().BoolVar(&makeDefault, "default", false, "Set this profile as default_profile")

	var forceInit bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a starter ~/.config/sapi/config.toml file",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := state.Overrides.ConfigPath
			if path == "" {
				var err error
				path, err = config.DefaultConfigPath()
				if err != nil {
					return err
				}
			}
			if !forceInit {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("config file %s already exists (use --force to overwrite)", path)
				}
			}
			verifyTrue := true
			verifyFalse := false
			sample := &config.Config{
				DefaultProfile: "prod",
				DefaultOutput:  "pretty",
				Profiles: map[string]config.Profile{
					"prod": {
						URL:         "https://salt-master.prod.example.com:8000",
						Username:    "saltadmin",
						PasswordEnv: "SALT_PROD_PASSWORD",
						EAuth:       "pam",
						VerifySSL:   &verifyTrue,
						Timeout:     "60s",
					},
					"staging": {
						URL:         "https://salt-master.staging.example.com:8000",
						Username:    "saltadmin",
						PasswordEnv: "SALT_STAGING_PASSWORD",
						EAuth:       "pam",
						VerifySSL:   &verifyFalse,
						Timeout:     "30s",
					},
				},
			}
			if err := config.Save(path, sample); err != nil {
				return err
			}
			fmt.Printf("Initialized starter config at %s\n", path)
			return nil
		},
	}
	initCmd.Flags().BoolVar(&forceInit, "force", false, "Overwrite existing config.toml")

	profileCmd.AddCommand(listCmd, showCmd, setDefaultCmd, setCmd, initCmd)
	return profileCmd
}

func newAuthCmd(state *AppState) *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Salt NetAPI authentication tokens (login, status, logout)",
	}

	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with /login and cache a fresh X-Auth-Token",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(true)
			if err != nil {
				return err
			}
			tok, err := client.Login(cmd.Context())
			if err != nil {
				return err
			}
			out := map[string]any{
				"profile":    resolved.Name,
				"url":        resolved.URL,
				"user":       tok.User,
				"eauth":      tok.EAuth,
				"expires_at": tok.ExpiresAt().Format(time.RFC3339),
				"ttl":        time.Until(tok.ExpiresAt()).Round(time.Second).String(),
				"perms":      tok.Perms,
			}
			return state.render(resolved, out)
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect cached X-Auth-Tokens and their expiration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := auth.NewTokenStore("")
			if err != nil {
				return err
			}
			all, err := store.ListAll()
			if err != nil {
				return err
			}
			out := make(map[string]any, len(all))
			for _, tok := range all {
				exp := tok.ExpiresAt()
				valid := tok.IsValid(auth.DefaultLeeway)
				ttlStr := "expired"
				if valid && !exp.IsZero() {
					ttlStr = time.Until(exp).Round(time.Second).String()
				}
				label := fmt.Sprintf("%s (%s@%s)", tok.Profile, tok.User, tok.URL)
				out[label] = map[string]any{
					"valid":      valid,
					"eauth":      tok.EAuth,
					"expires_at": exp.Format(time.RFC3339),
					"ttl":        ttlStr,
					"perms":      tok.Perms,
				}
			}
			return state.render(nil, out)
		},
	}

	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Invalidate current token via /logout and clear cached entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, resolved, err := state.buildClient(false)
			if err != nil {
				return err
			}
			if err := client.Logout(cmd.Context()); err != nil {
				return err
			}
			fmt.Printf("Logged out from profile %q (%s)\n", resolved.Name, resolved.URL)
			return nil
		},
	}

	authCmd.AddCommand(loginCmd, statusCmd, logoutCmd)
	return authCmd
}
