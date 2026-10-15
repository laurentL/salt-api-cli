package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPreprocessArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "empty",
			in:   []string{},
			want: []string{},
		},
		{
			name: "direct salt shortcut",
			in:   []string{"*", "test.ping"},
			want: []string{"run", "*", "test.ping"},
		},
		{
			name: "direct salt shortcut with profile and grain flags",
			in:   []string{"-p", "prod", "-G", "os:Debian", "cmd.run", "uptime"},
			want: []string{"run", "-p", "prod", "-G", "os:Debian", "cmd.run", "uptime"},
		},
		{
			name: "structured subcommand job list",
			in:   []string{"job", "list"},
			want: []string{"job", "list"},
		},
		{
			name: "structured subcommand with leading flag",
			in:   []string{"-p", "prod", "key", "list"},
			want: []string{"-p", "prod", "key", "list"},
		},
		{
			name: "mcp flag",
			in:   []string{"--mcp"},
			want: []string{"mcp"},
		},
		{
			name: "help flag",
			in:   []string{"--help"},
			want: []string{"--help"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PreprocessArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("PreprocessArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestGenerateManPages(t *testing.T) {
	tmpDir := t.TempDir()
	rootCmd := NewRootCmd()
	if err := GenerateManPages(rootCmd, tmpDir); err != nil {
		t.Fatalf("GenerateManPages failed: %v", err)
	}

	expected := []string{
		"sapi.1",
		"sapi-run.1",
		"sapi-async.1",
		"sapi-batch.1",
		"sapi-job.1",
		"sapi-minion.1",
		"sapi-runner.1",
		"sapi-wheel.1",
		"sapi-key.1",
		"sapi-event.1",
		"sapi-profile.1",
		"sapi-auth.1",
		"sapi-stats.1",
		"sapi-mcp.1",
	}

	for _, fname := range expected {
		data, err := os.ReadFile(filepath.Join(tmpDir, fname))
		if err != nil {
			t.Fatalf("expected man page %s to exist: %v", fname, err)
		}
		if !strings.HasPrefix(string(data), ".TH ") {
			t.Errorf("man page %s does not start with .TH header", fname)
		}
	}
}
