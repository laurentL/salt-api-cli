package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
)

func colorize(s, color string, enabled bool) string {
	if !enabled {
		return s
	}
	return color + s + ansiReset
}

func renderPretty(w io.Writer, data any, color bool) {
	if m, ok := data.(map[string]any); ok {
		keys := sortedKeys(m)
		for _, k := range keys {
			v := m[k]
			headerColor := ansiCyan + ansiBold
			if b, isBool := v.(bool); isBool && !b {
				headerColor = ansiRed + ansiBold
			}
			fmt.Fprintf(w, "%s:\n", colorize(k, headerColor, color))
			if subMap, isMap := v.(map[string]any); isMap && isHighstateMap(subMap) {
				renderHighstate(w, k, subMap, color)
				continue
			}
			renderNestedValue(w, v, 1, color)
		}
		return
	}
	renderNestedValue(w, data, 0, color)
}

func renderNestedValue(w io.Writer, val any, indent int, color bool) {
	prefix := strings.Repeat("    ", indent)

	switch v := val.(type) {
	case nil:
		fmt.Fprintf(w, "%s%s\n", prefix, colorize("None", ansiYellow, color))

	case bool:
		if v {
			fmt.Fprintf(w, "%s%s\n", prefix, colorize("True", ansiGreen+ansiBold, color))
		} else {
			fmt.Fprintf(w, "%s%s\n", prefix, colorize("False", ansiRed+ansiBold, color))
		}

	case int, int64, float64:
		fmt.Fprintf(w, "%s%s\n", prefix, colorize(fmt.Sprintf("%v", v), ansiYellow, color))

	case string:
		lines := strings.Split(v, "\n")
		for _, line := range lines {
			fmt.Fprintf(w, "%s%s\n", prefix, colorize(line, ansiGreen, color))
		}

	case []string:
		if len(v) == 0 {
			fmt.Fprintf(w, "%s[]\n", prefix)
			return
		}
		for _, item := range v {
			fmt.Fprintf(w, "%s%s %s\n", prefix, colorize("-", ansiCyan, color), colorize(item, ansiGreen, color))
		}

	case []any:
		if len(v) == 0 {
			fmt.Fprintf(w, "%s[]\n", prefix)
			return
		}
		for _, item := range v {
			switch scalar := item.(type) {
			case string, bool, int, int64, float64, nil:
				fmt.Fprintf(w, "%s%s %v\n", prefix, colorize("-", ansiCyan, color), formatInlineScalar(scalar, color))
			default:
				fmt.Fprintf(w, "%s%s\n", prefix, colorize("-", ansiCyan, color))
				renderNestedValue(w, item, indent+1, color)
			}
		}

	case map[string]any:
		if len(v) == 0 {
			fmt.Fprintf(w, "%s{}\n", prefix)
			return
		}
		keys := sortedKeys(v)
		for _, k := range keys {
			sub := v[k]
			switch scalar := sub.(type) {
			case string:
				if !strings.Contains(scalar, "\n") {
					fmt.Fprintf(w, "%s%s: %s\n", prefix, colorize(k, ansiBlue, color), colorize(scalar, ansiGreen, color))
				} else {
					fmt.Fprintf(w, "%s%s:\n", prefix, colorize(k, ansiBlue, color))
					renderNestedValue(w, sub, indent+1, color)
				}
			case bool, int, int64, float64, nil:
				fmt.Fprintf(w, "%s%s: %s\n", prefix, colorize(k, ansiBlue, color), formatInlineScalar(scalar, color))
			default:
				fmt.Fprintf(w, "%s%s:\n", prefix, colorize(k, ansiBlue, color))
				renderNestedValue(w, sub, indent+1, color)
			}
		}

	default:
		fmt.Fprintf(w, "%s%v\n", prefix, v)
	}
}

func formatInlineScalar(v any, color bool) string {
	switch tv := v.(type) {
	case nil:
		return colorize("None", ansiYellow, color)
	case bool:
		if tv {
			return colorize("True", ansiGreen+ansiBold, color)
		}
		return colorize("False", ansiRed+ansiBold, color)
	case int, int64, float64:
		return colorize(fmt.Sprintf("%v", tv), ansiYellow, color)
	case string:
		return colorize(tv, ansiGreen, color)
	default:
		return fmt.Sprintf("%v", tv)
	}
}

func isHighstateMap(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for k, v := range m {
		if strings.Count(k, "_|-") == 3 {
			if _, ok := v.(map[string]any); ok {
				return true
			}
		}
	}
	return false
}

func renderHighstate(w io.Writer, minion string, states map[string]any, color bool) {
	keys := sortedKeys(states)
	var succeeded, unchanged, failed int
	var totalDuration float64

	for _, key := range keys {
		parts := strings.Split(key, "_|-")
		stateMod := parts[0]
		stateID := key
		stateName := ""
		stateFun := ""
		if len(parts) == 4 {
			stateID = parts[1]
			stateName = parts[2]
			stateFun = parts[0] + "." + parts[3]
		} else {
			_ = stateMod
		}

		stMap, ok := states[key].(map[string]any)
		if !ok {
			continue
		}

		var resPtr *bool
		if r, ok := stMap["result"].(bool); ok {
			resPtr = &r
		}
		comment, _ := stMap["comment"].(string)
		duration, _ := stMap["duration"].(float64)
		totalDuration += duration

		stColor := ansiGreen
		resStr := "true"
		if resPtr == nil {
			stColor = ansiYellow
			resStr = "None (test=True)"
			unchanged++
		} else if *resPtr {
			succeeded++
		} else {
			stColor = ansiRed
			resStr = "false"
			failed++
		}

		fmt.Fprintf(w, "----------\n")
		fmt.Fprintf(w, "          ID: %s\n", colorize(stateID, stColor+ansiBold, color))
		fmt.Fprintf(w, "    Function: %s\n", colorize(stateFun, stColor, color))
		if stateName != "" && stateName != stateID {
			fmt.Fprintf(w, "        Name: %s\n", stateName)
		}
		fmt.Fprintf(w, "      Result: %s\n", colorize(resStr, stColor+ansiBold, color))
		fmt.Fprintf(w, "     Comment: %s\n", comment)
		if duration > 0 {
			fmt.Fprintf(w, "    Duration: %.2f ms\n", duration)
		}
		if changes, ok := stMap["changes"].(map[string]any); ok && len(changes) > 0 {
			fmt.Fprintf(w, "     Changes:\n")
			renderNestedValue(w, changes, 3, color)
		}
	}

	fmt.Fprintf(w, "\nSummary for %s\n------------\n", colorize(minion, ansiCyan+ansiBold, color))
	succLabel := fmt.Sprintf("%d", succeeded)
	if unchanged > 0 {
		succLabel = fmt.Sprintf("%d (unchanged=%d)", succeeded+unchanged, unchanged)
	}
	fmt.Fprintf(w, "Succeeded: %s\n", colorize(succLabel, ansiGreen+ansiBold, color))
	failColor := ansiGreen
	if failed > 0 {
		failColor = ansiRed + ansiBold
	}
	fmt.Fprintf(w, "Failed:    %s\n", colorize(fmt.Sprintf("%d", failed), failColor, color))
	fmt.Fprintf(w, "------------\n")
	fmt.Fprintf(w, "Total states run:     %d\n", succeeded+unchanged+failed)
	fmt.Fprintf(w, "Total run time:   %.2f ms\n", totalDuration)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
