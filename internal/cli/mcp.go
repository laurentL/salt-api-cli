package cli

import (
	"github.com/laurentL/salt-api-cli/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd(state *AppState) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the integrated Full-Access Salt MCP server over stdio",
		Long: `Starts a Model Context Protocol (MCP) server on stdin/stdout exposing Full-Access
Salt NetAPI tools (execution, state apply, jobs, minions/grains, runners, wheel, key management, webhooks)
for AI agents such as Antigravity (agy), Gemini CLI, and Claude.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(state)
		},
	}
}

func runMCPServer(state *AppState) error {
	return mcp.ServeStdio(state.Overrides, Version)
}
