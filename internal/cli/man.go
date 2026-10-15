package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newManCmd(rootCmd *cobra.Command) *cobra.Command {
	var outDir string
	cmd := &cobra.Command{
		Use:   "man",
		Short: "Generate roff man pages for sapi and its subcommands",
		Example: `  sapi man --dir ./man
  man ./man/sapi.1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := GenerateManPages(rootCmd, outDir); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated man pages in %s\n", outDir)
			return nil
		},
	}
	cmd.Flags().StringVarP(&outDir, "dir", "d", "man", "Output directory for generated .1 man pages")
	return cmd
}

// GenerateManPages writes the primary sapi.1 manual page and per-subcommand sapi-<cmd>.1 pages.
func GenerateManPages(rootCmd *cobra.Command, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("failed to create man directory %s: %w", outDir, err)
	}
	dateStr := time.Now().UTC().Format("2006-01-02")

	// 1. Write primary comprehensive sapi.1
	mainMan := renderRootManPage(rootCmd, dateStr)
	mainPath := filepath.Join(outDir, "sapi.1")
	if err := os.WriteFile(mainPath, []byte(mainMan), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", mainPath, err)
	}

	// 2. Write subcommand man pages sapi-<subcommand>.1
	for _, sub := range rootCmd.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" || sub.Name() == "version" || sub.Name() == "man" {
			continue
		}
		subMan := renderSubcommandManPage(rootCmd, sub, dateStr)
		subPath := filepath.Join(outDir, fmt.Sprintf("sapi-%s.1", sub.Name()))
		if err := os.WriteFile(subPath, []byte(subMan), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", subPath, err)
		}
	}

	return nil
}

func renderRootManPage(rootCmd *cobra.Command, dateStr string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, `.TH "SAPI" "1" "%s" "sapi %s" "Salt API CLI Manual"
.SH NAME
sapi \- SaltStack salt-api hybrid CLI client and Full-Access MCP Server
.SH SYNOPSIS
.B sapi
[\fIGLOBAL FLAGS\fR] \fI<target>\fR \fI<function>\fR [\fIargs...\fR] [\fIkey=value...\fR]
.br
.B sapi
[\fIGLOBAL FLAGS\fR] \fI<subcommand>\fR [\fISUBCOMMAND FLAGS\fR] [\fIargs...\fR]
.br
.B sapi
\fBmcp\fR
.SH DESCRIPTION
\fBsapi\fR is a fast, standalone command-line client wrapping SaltStack's \fBsalt-api\fR (\fBnetapi\fR: \fBrest_cherrypy\fR / \fBrest_tornado\fR).
It provides both a drop-in Salt-native CLI shortcut (\fBsapi '*' test.ping\fR) and structured subcommands for synchronous, asynchronous, and batch executions, job inspection, minion grain queries, Master runners, Wheel key management, real-time Server-Sent Events (SSE), webhooks, multi-profile management, and an embedded Model Context Protocol (MCP) server over \fBstdio\fR.
.SH USAGE MODES
.TP
\fB1. Direct Salt-Native Shortcut Mode\fR
If the first positional argument is not a reserved subcommand, \fBsapi\fR automatically treats it as a target expression followed by a Salt execution module and optional arguments:
.PP
.nf
.RS
sapi '*' test.ping
sapi -G 'os_family:Debian' pkg.install htop refresh=True
sapi -L 'web-01,web-02' service.restart nginx
sapi --async --wait '*' state.apply common test=True
sapi -b 25%% '*' state.highstate
.RE
.fi
.TP
\fB2. Structured Subcommand Mode\fR
Explicitly invoke subcommands (\fBrun\fR, \fBasync\fR, \fBbatch\fR, \fBjob\fR, \fBminion\fR, \fBrunner\fR, \fBwheel\fR, \fBkey\fR, \fBevent\fR, \fBprofile\fR, \fBauth\fR, \fBstats\fR, \fBmcp\fR).
.TP
\fB3. Full-Access MCP Server Mode\fR
Run \fBsapi mcp\fR (or \fBsapi --mcp\fR) to start a JSON-RPC 2.0 Model Context Protocol server over \fBstdio\fR for AI coding and operations agents (Claude Code, Gemini CLI, Cursor, Google Antigravity).
.SH GLOBAL OPTIONS
`, dateStr, roffEscape(Version))

	writeFlagSetRoff(&b, rootCmd.PersistentFlags())

	b.WriteString(`.SH TARGETING OPTIONS (RUN / SHORTCUT MODE)
.TP
\fB\-G\fR, \fB\-\-grain\fR
Match targets using Salt Grains (e.g. \fB'os_family:Debian'\fR).
.TP
\fB\-E\fR, \fB\-\-pcre\fR
Match targets using PCRE regular expressions (e.g. \fB'web\-(prod|stg)\-[0-9]+'\fR).
.TP
\fB\-L\fR, \fB\-\-list\fR
Match targets as a comma-separated list of minion IDs (e.g. \fB'minion1,minion2'\fR).
.TP
\fB\-I\fR, \fB\-\-pillar\fR
Match targets using Salt Pillar values.
.TP
\fB\-C\fR, \fB\-\-compound\fR
Match targets using a Salt compound expression (e.g. \fB'G@os:Debian and not L@web-01'\fR).
.TP
\fB\-N\fR, \fB\-\-nodegroup\fR
Match targets using a Salt Master nodegroup name.
.TP
\fB\-S\fR, \fB\-\-ipcidr\fR
Match targets by subnet or IPv4/IPv6 CIDR (e.g. \fB'10.0.1.0/24'\fR).
.TP
\fB\-b\fR, \fB\-\-batch\fR \fISIZE\fR
Execute progressively in batches (e.g. \fB25%%\fR or \fB5\fR) via \fBlocal_batch\fR.
.TP
\fB\-\-async\fR
Dispatch asynchronously via \fBlocal_async\fR and return the Job ID (\fBjid\fR).
.TP
\fB\-\-wait\fR
Wait and poll \fB/jobs/<jid>\fR until all targeted minions return or timeout expires.
.SH SUBCOMMANDS
`)

	for _, sub := range rootCmd.Commands() {
		if sub.Hidden || sub.Name() == "help" {
			continue
		}
		fmt.Fprintf(&b, ".TP\n\\fBsapi %s\\fR\n%s\n", roffEscape(sub.Name()), roffEscape(sub.Short))
	}

	b.WriteString(`.SH MCP SERVER TOOLS
When started with \fBsapi mcp\fR, the following 12 Full-Access tools are exposed over \fBstdio\fR (all tools accept an optional \fBprofile\fR parameter):
.IP "\[bu]" 2
\fBsalt_list_profiles\fR \- List configured Salt Master profiles and default profile.
.IP "\[bu]" 2
\fBsalt_ping\fR \- Test connectivity (\fBtest.ping\fR) on targeted minions.
.IP "\[bu]" 2
\fBsalt_run\fR \- Execute any Salt execution module (\fBlocal\fR, \fBlocal_async\fR, \fBlocal_batch\fR).
.IP "\[bu]" 2
\fBsalt_state_apply\fR \- Apply a Salt state or \fBhighstate\fR (\fBtest\fR, \fBpillar\fR, \fBsaltenv\fR, \fBwait\fR).
.IP "\[bu]" 2
\fBsalt_list_minions\fR \- List known minions and grains or online/offline status (\fBmanage.status\fR).
.IP "\[bu]" 2
\fBsalt_get_minion_grains\fR \- Retrieve full or filtered grains for a minion.
.IP "\[bu]" 2
\fBsalt_list_jobs\fR \- List job history (\fB/jobs\fR) or currently active jobs (\fBjobs.active\fR).
.IP "\[bu]" 2
\fBsalt_get_job\fR \- Retrieve detailed job return data by \fBjid\fR (with optional \fBwait=true\fR).
.IP "\[bu]" 2
\fBsalt_runner\fR \- Execute any Salt Master Runner module (\fBmanage.*\fR, \fBstate.orchestrate\fR, etc.).
.IP "\[bu]" 2
\fBsalt_wheel\fR \- Execute any Salt Master Wheel function.
.IP "\[bu]" 2
\fBsalt_keys_manage\fR \- Manage minion keys (\fBlist\fR, \fBaccept\fR, \fBreject\fR, \fBdelete\fR, \fBfinger\fR).
.IP "\[bu]" 2
\fBsalt_send_hook\fR \- Trigger a Salt-API webhook (\fBPOST /hook/<tag>\fR) with a JSON payload.
.SH ENVIRONMENT
.TP
\fBSALT_API_PROFILE\fR
Active profile name from \fBconfig.toml\fR.
.TP
\fBSALT_API_CONFIG\fR
Custom path to \fBconfig.toml\fR (default: \fB~/.config/sapi/config.toml\fR).
.TP
\fBSALT_API_URL\fR
Salt Master \fBsalt-api\fR base URL (e.g. \fBhttps://salt-master:8000\fR).
.TP
\fBSALT_API_USER\fR
Salt API username.
.TP
\fBSALT_API_PASSWORD\fR
Salt API password.
.TP
\fBSALT_API_EAUTH\fR
Salt external authentication backend (\fBpam\fR, \fBldap\fR, \fBfile\fR, \fBsharedsecret\fR).
.TP
\fBSALT_API_TOKEN\fR
Explicit \fBX-Auth-Token\fR override.
.TP
\fBSALT_API_INSECURE\fR
Set to \fBtrue\fR or \fB1\fR to skip TLS certificate verification.
.TP
\fBSALT_API_OUTPUT\fR
Default output format (\fBpretty\fR, \fBjson\fR, \fByaml\fR, \fBtable\fR).
.SH FILES
.TP
\fI~/.config/sapi/config.toml\fR
Multi-profile TOML configuration file.
.TP
\fI~/.cache/sapi/tokens.json\fR
Cached \fBX-Auth-Token\fR store (permissions \fB0600\fR).
.TP
\fI/tmp/sapi_mcp.log\fR
Technical log file written when running \fBsapi mcp\fR.
.SH AUTHOR
Laurent Labatut <https://github.com/laurentL/salt-api-cli>
.SH SEE ALSO
\fBsapi-run\fR(1), \fBsapi-job\fR(1), \fBsapi-minion\fR(1), \fBsapi-key\fR(1), \fBsapi-runner\fR(1), \fBsapi-wheel\fR(1), \fBsapi-event\fR(1), \fBsapi-profile\fR(1), \fBsapi-auth\fR(1), \fBsapi-mcp\fR(1)
`)
	return b.String()
}

func renderSubcommandManPage(rootCmd, sub *cobra.Command, dateStr string) string {
	var b bytes.Buffer
	title := strings.ToUpper(fmt.Sprintf("SAPI-%s", sub.Name()))
	fmt.Fprintf(&b, `.TH "%s" "1" "%s" "sapi %s" "Salt API CLI Manual"
.SH NAME
sapi-%s \- %s
.SH SYNOPSIS
.B sapi %s
`, title, dateStr, roffEscape(Version), roffEscape(sub.Name()), roffEscape(sub.Short), roffEscape(sub.UseLine()))

	desc := sub.Long
	if desc == "" {
		desc = sub.Short
	}
	fmt.Fprintf(&b, ".SH DESCRIPTION\n%s\n", roffEscape(desc))

	if len(sub.Commands()) > 0 {
		b.WriteString(".SH SUBCOMMANDS\n")
		for _, child := range sub.Commands() {
			if child.Hidden || child.Name() == "help" {
				continue
			}
			fmt.Fprintf(&b, ".TP\n\\fBsapi %s %s\\fR\n%s\n", roffEscape(sub.Name()), roffEscape(child.Use), roffEscape(child.Short))
		}
	}

	if sub.Flags().HasAvailableFlags() {
		b.WriteString(".SH OPTIONS\n")
		writeFlagSetRoff(&b, sub.Flags())
	}

	b.WriteString(".SH GLOBAL OPTIONS\n")
	writeFlagSetRoff(&b, rootCmd.PersistentFlags())

	if strings.TrimSpace(sub.Example) != "" {
		b.WriteString(".SH EXAMPLES\n.nf\n.RS\n")
		b.WriteString(roffEscape(sub.Example))
		b.WriteString("\n.RE\n.fi\n")
	}

	b.WriteString(".SH SEE ALSO\n\\fBsapi\\fR(1)\n")
	return b.String()
}

func writeFlagSetRoff(b *bytes.Buffer, fs *pflag.FlagSet) {
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		var flagSpec string
		if f.Shorthand != "" {
			flagSpec = fmt.Sprintf("\\fB\\-%s\\fR, \\fB\\-\\-%s\\fR", roffEscape(f.Shorthand), roffEscape(f.Name))
		} else {
			flagSpec = fmt.Sprintf("\\fB\\-\\-%s\\fR", roffEscape(f.Name))
		}
		if f.Value.Type() != "bool" {
			flagSpec += fmt.Sprintf(" \\fI%s\\fR", strings.ToUpper(roffEscape(f.Value.Type())))
		}
		fmt.Fprintf(b, ".TP\n%s\n%s\n", flagSpec, roffEscape(f.Usage))
	})
}

func roffEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `-`, `\-`)
	return s
}
