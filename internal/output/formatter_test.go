package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderFormats(t *testing.T) {
	sample := map[string]any{
		"web-01": true,
		"web-02": false,
	}

	var buf bytes.Buffer
	if err := Render(sample, Options{Format: FormatPretty, NoColor: true, Writer: &buf}); err != nil {
		t.Fatalf("Render pretty failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "web-01:\n    True") || !strings.Contains(out, "web-02:\n    False") {
		t.Errorf("unexpected pretty output:\n%s", out)
	}

	// Highstate detection and formatting
	highstateSample := map[string]any{
		"web-01": map[string]any{
			"pkg_|-nginx_pkg_|-nginx_|-installed": map[string]any{
				"result":   true,
				"comment":  "Package nginx is already installed",
				"duration": 12.5,
				"changes":  map[string]any{},
			},
		},
	}
	buf.Reset()
	if err := Render(highstateSample, Options{Format: FormatPretty, NoColor: true, Writer: &buf}); err != nil {
		t.Fatalf("Render highstate failed: %v", err)
	}
	hsOut := buf.String()
	if !strings.Contains(hsOut, "ID: nginx_pkg") || !strings.Contains(hsOut, "Summary for web-01") {
		t.Errorf("expected highstate summary, got:\n%s", hsOut)
	}

	// Table format
	buf.Reset()
	if err := Render(sample, Options{Format: FormatTable, NoColor: true, Writer: &buf}); err != nil {
		t.Fatalf("Render table failed: %v", err)
	}
	if !strings.Contains(buf.String(), "KEY") || !strings.Contains(buf.String(), "web-01") {
		t.Errorf("unexpected table output:\n%s", buf.String())
	}
}
