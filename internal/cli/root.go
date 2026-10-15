package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/laurentL/salt-api-cli/internal/auth"
	"github.com/laurentL/salt-api-cli/internal/config"
	"github.com/laurentL/salt-api-cli/internal/output"
	"github.com/laurentL/salt-api-cli/internal/saltclient"
	"github.com/spf13/cobra"
)

var Version = "1.0.0"

// AppState holds global CLI flags and resolved runtime helpers.
type AppState struct {
	Overrides config.Overrides
	NoColor   bool
	MCPFlag   bool
}

var knownSubcommands = map[string]bool{
	"run":        true,
	"async":      true,
	"batch":      true,
	"job":        true,
	"jobs":       true,
	"minion":     true,
	"minions":    true,
	"runner":     true,
	"wheel":      true,
	"key":        true,
	"keys":       true,
	"event":      true,
	"events":     true,
	"profile":    true,
	"profiles":   true,
	"auth":       true,
	"stats":      true,
	"mcp":        true,
	"man":        true,
	"version":    true,
	"help":       true,
	"completion": true,
}

// flagsTakingValue lists short/long flags that consume the next argument when not passed with '='.
var flagsTakingValue = map[string]bool{
	"-p":              true,
	"--profile":       true,
	"-c":              true,
	"--config":        true,
	"-u":              true,
	"--url":           true,
	"--user":          true,
	"--password":      true,
	"--eauth":         true,
	"--token":         true,
	"--ca-bundle":     true,
	"-t":              true,
	"--timeout":       true,
	"-o":              true,
	"--output":        true,
	"-b":              true,
	"--batch":         true,
	"--tgt-type":      true,
	"--wait-interval": true,
}

// NewRootCmd constructs the root Cobra command for sapi.
func NewRootCmd() *cobra.Command {
	state := &AppState{}

	rootCmd := &cobra.Command{
		Use:   "sapi [command | <target> <function> [args...]]",
		Short: "Salt NetAPI (salt-api) hybrid CLI client and Full-Access MCP Server",
		Long: `sapi wraps SaltStack's salt-api (rest_cherrypy / rest_tornado) in a fast single binary.

It supports three usage modes:
  1. Direct Salt-native shortcut:
       sapi '*' test.ping
       sapi -G 'os_family:Debian' pkg.install htop refresh=True
       sapi -b 25% '*' state.highstate

  2. Structured subcommands:
       sapi run '*' cmd.run 'uptime'
       sapi job list / job get <jid> / job wait <jid>
       sapi minion list / minion grains <id> / minion status
       sapi key list / key accept <id> / key delete <id>
       sapi runner manage.status
       sapi event listen --tag 'salt/job/*'
       sapi profile list / auth login

  3. Model Context Protocol (MCP) Server for AI Agents (Antigravity, Gemini, Claude):
       sapi mcp`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			if state.MCPFlag {
				return runMCPServer(state)
			}
			return cmd.Help()
		},
	}

	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&state.Overrides.Profile, "profile", "p", "", "Configuration profile name (env: SALT_API_PROFILE)")
	pf.StringVarP(&state.Overrides.ConfigPath, "config", "c", "", "Path to config.toml (default: ~/.config/sapi/config.toml)")
	pf.StringVarP(&state.Overrides.URL, "url", "u", "", "Salt API base URL, e.g. https://salt:8000 (env: SALT_API_URL)")
	pf.StringVar(&state.Overrides.Username, "user", "", "Salt API username (env: SALT_API_USER)")
	pf.StringVar(&state.Overrides.Password, "password", "", "Salt API password (env: SALT_API_PASSWORD)")
	pf.StringVar(&state.Overrides.EAuth, "eauth", "", "Salt external auth backend: pam, ldap, file, sharedsecret (env: SALT_API_EAUTH)")
	pf.StringVar(&state.Overrides.Token, "token", "", "Explicit X-Auth-Token override (env: SALT_API_TOKEN)")
	pf.BoolVarP(&state.Overrides.Insecure, "insecure", "k", false, "Skip TLS certificate verification (env: SALT_API_INSECURE)")
	pf.StringVar(&state.Overrides.CABundle, "ca-bundle", "", "Path to custom CA certificate bundle PEM (env: SALT_API_CA_BUNDLE)")
	pf.DurationVarP(&state.Overrides.Timeout, "timeout", "t", 0, "HTTP request timeout, e.g. 60s (env: SALT_API_TIMEOUT)")
	pf.StringVarP(&state.Overrides.Output, "output", "o", "", "Output format: pretty, json, yaml, table (env: SALT_API_OUTPUT)")
	pf.BoolVar(&state.NoColor, "no-color", false, "Disable ANSI color output")
	pf.BoolVar(&state.MCPFlag, "mcp", false, "Start the integrated MCP server over stdio")

	rootCmd.AddCommand(
		newRunCmd(state),
		newAsyncCmd(state),
		newBatchCmd(state),
		newJobCmd(state),
		newMinionCmd(state),
		newRunnerCmd(state),
		newWheelCmd(state),
		newKeyCmd(state),
		newEventCmd(state),
		newProfileCmd(state),
		newAuthCmd(state),
		newStatsCmd(state),
		newMCPCmd(state),
		newManCmd(rootCmd),
		newVersionCmd(),
	)

	return rootCmd
}

// Execute runs the CLI with automatic Salt-native shortcut detection.
func Execute() error {
	rootCmd := NewRootCmd()
	args := PreprocessArgs(os.Args[1:])
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

// PreprocessArgs detects when the user invokes `sapi '<tgt>' <fun> [args...]` directly
// without specifying `run`, and rewrites the argument slice to invoke `run`.
func PreprocessArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	for _, a := range args {
		if a == "--mcp" {
			return []string{"mcp"}
		}
		if a == "-h" || a == "--help" || a == "-v" || a == "--version" {
			return args
		}
	}

	firstPositional := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				firstPositional = args[i+1]
			}
			break
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && flagsTakingValue[arg] {
				i++ // skip flag value
			}
			continue
		}
		firstPositional = arg
		break
	}

	if firstPositional == "" || knownSubcommands[firstPositional] {
		return args
	}

	// Prepend "run" so Cobra routes `sapi [flags] <tgt> <fun> [args...]` to the `run` subcommand
	out := make([]string, 0, len(args)+1)
	out = append(out, "run")
	out = append(out, args...)
	return out
}

func (s *AppState) buildClient(allowPrompt bool) (*saltclient.Client, *config.ResolvedProfile, error) {
	resolved, _, err := config.Resolve(s.Overrides)
	if err != nil {
		return nil, nil, err
	}
	store, err := auth.NewTokenStore("")
	if err != nil {
		return nil, nil, err
	}
	client, err := saltclient.New(resolved, store, allowPrompt)
	if err != nil {
		return nil, nil, err
	}
	return client, resolved, nil
}

func (s *AppState) render(resolved *config.ResolvedProfile, data any) error {
	fmtStr := s.Overrides.Output
	if fmtStr == "" && resolved != nil {
		fmtStr = resolved.Output
	}
	if fmtStr == "" {
		fmtStr = string(output.FormatPretty)
	}
	return output.Render(data, output.Options{
		Format:  output.Format(fmtStr),
		NoColor: s.NoColor,
		Writer:  os.Stdout,
	})
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print sapi version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("sapi version %s\n", Version)
		},
	}
}
