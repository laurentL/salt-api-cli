package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// Format specifies the output rendering mode.
type Format string

const (
	FormatPretty Format = "pretty"
	FormatJSON   Format = "json"
	FormatYAML   Format = "yaml"
	FormatTable  Format = "table"
)

// Options controls output rendering behavior.
type Options struct {
	Format  Format
	NoColor bool
	Writer  io.Writer
}

// Render prints data to opts.Writer (or os.Stdout) according to opts.Format.
func Render(data any, opts Options) error {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}

	fmtMode := Format(strings.ToLower(strings.TrimSpace(string(opts.Format))))
	if fmtMode == "" {
		fmtMode = FormatPretty
	}

	useColor := !opts.NoColor && os.Getenv("NO_COLOR") == "" && isTerminalWriter(w)

	switch fmtMode {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)

	case FormatYAML:
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		defer enc.Close()
		return enc.Encode(data)

	case FormatTable:
		return renderTable(w, data)

	case FormatPretty:
		fallthrough
	default:
		renderPretty(w, data, useColor)
		return nil
	}
}

func isTerminalWriter(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func renderTable(w io.Writer, data any) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	defer tw.Flush()

	switch v := data.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintln(tw, "KEY\tVALUE")
		fmt.Fprintln(tw, "---\t-----")
		for _, k := range keys {
			val := v[k]
			fmt.Fprintf(tw, "%s\t%s\n", k, compactValue(val))
		}
		return nil

	case []any:
		fmt.Fprintln(tw, "INDEX\tVALUE")
		fmt.Fprintln(tw, "-----\t-----")
		for i, item := range v {
			fmt.Fprintf(tw, "%d\t%s\n", i, compactValue(item))
		}
		return nil

	default:
		fmt.Fprintf(tw, "%v\n", data)
		return nil
	}
}

func compactValue(val any) string {
	switch tv := val.(type) {
	case nil:
		return "None"
	case string:
		return strings.ReplaceAll(tv, "\n", " ")
	case bool, int, int64, float64:
		return fmt.Sprintf("%v", tv)
	default:
		b, err := json.Marshal(tv)
		if err != nil {
			return fmt.Sprintf("%v", tv)
		}
		return string(b)
	}
}
