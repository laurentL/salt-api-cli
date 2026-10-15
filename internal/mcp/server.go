package mcp

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/laurentL/salt-api-cli/internal/config"
)

const DefaultLogPath = "/tmp/sapi_mcp.log"

// NewServer constructs the MCPServer instance with all 12 Full-Access Salt tools registered.
func NewServer(baseOverrides config.Overrides, version string, logger *log.Logger) *MCPServer {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	s := NewMCPServer("sapi", version, logger)
	RegisterTools(s, baseOverrides, logger)
	return s
}

// ServeStdio starts the MCP server on stdin/stdout and logs technical activity to /tmp/sapi_mcp.log.
func ServeStdio(baseOverrides config.Overrides, version string) error {
	logFile, err := os.OpenFile(DefaultLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	var logger *log.Logger
	if err == nil {
		defer logFile.Close()
		logger = log.New(logFile, "[sapi-mcp] ", log.LstdFlags)
	} else {
		logger = log.New(io.Discard, "", 0)
	}

	logger.Printf("Starting sapi Full-Access MCP Server v%s...", version)
	s := NewServer(baseOverrides, version, logger)
	return s.ServeReaderWriter(context.Background(), os.Stdin, os.Stdout)
}
