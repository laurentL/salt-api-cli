package saltclient

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// TargetType defines how Salt matches minion targets.
type TargetType string

const (
	TargetGlob       TargetType = "glob"
	TargetPCRE       TargetType = "pcre"
	TargetList       TargetType = "list"
	TargetGrain      TargetType = "grain"
	TargetGrainPCRE  TargetType = "grain_pcre"
	TargetPillar     TargetType = "pillar"
	TargetPillarPCRE TargetType = "pillar_pcre"
	TargetNodegroup  TargetType = "nodegroup"
	TargetRange      TargetType = "range"
	TargetCompound   TargetType = "compound"
	TargetIPCIDR     TargetType = "ipcidr"
)

// ClientType defines the Salt NetAPI execution client.
type ClientType string

const (
	ClientLocal       ClientType = "local"
	ClientLocalAsync  ClientType = "local_async"
	ClientLocalBatch  ClientType = "local_batch"
	ClientRunner      ClientType = "runner"
	ClientRunnerAsync ClientType = "runner_async"
	ClientWheel       ClientType = "wheel"
	ClientWheelAsync  ClientType = "wheel_async"
)

// LowState represents a Salt NetAPI execution payload sent to POST /.
type LowState struct {
	Client     ClientType     `json:"client"`
	Tgt        any            `json:"tgt,omitempty"`
	Fun        string         `json:"fun"`
	Arg        []any          `json:"arg,omitempty"`
	Kwarg      map[string]any `json:"kwarg,omitempty"`
	TgtType    TargetType     `json:"tgt_type,omitempty"`
	Batch      string         `json:"batch,omitempty"`
	Timeout    int            `json:"timeout,omitempty"`
	FullReturn bool           `json:"full_return,omitempty"`
	Match      any            `json:"match,omitempty"` // Used by wheel functions such as key.accept / key.delete
}

// LoginRequest is the payload sent to POST /login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	EAuth    string `json:"eauth"`
}

// TokenInfo represents a single entry in the /login response's "return" array.
type TokenInfo struct {
	Token  string  `json:"token"`
	Start  float64 `json:"start"`
	Expire float64 `json:"expire"`
	User   string  `json:"user"`
	EAuth  string  `json:"eauth"`
	Perms  []any   `json:"perms"`
}

// LoginResponse wraps the response from POST /login.
type LoginResponse struct {
	Return []TokenInfo `json:"return"`
}

// RawEnvelope is the standard {"return": [...]} envelope returned by salt-api.
type RawEnvelope struct {
	Return []json.RawMessage `json:"return"`
}

// AsyncResult holds the JID and targeted minions returned by local_async / runner_async / wheel_async.
type AsyncResult struct {
	JID     string   `json:"jid"`
	Minions []string `json:"minions,omitempty"`
	Tag     string   `json:"tag,omitempty"`
}

// JobDetail represents detailed job data returned by GET /jobs/<jid>.
type JobDetail struct {
	JID        string         `json:"jid,omitempty"`
	Function   string         `json:"Function,omitempty"`
	Arguments  []any          `json:"Arguments,omitempty"`
	Kwargs     map[string]any `json:"Kwargs,omitempty"`
	Target     any            `json:"Target,omitempty"`
	TargetType string         `json:"Target-type,omitempty"`
	User       string         `json:"User,omitempty"`
	StartTime  string         `json:"StartTime,omitempty"`
	Minions    []string       `json:"Minions,omitempty"`
	Result     map[string]any `json:"Result,omitempty"`
	Raw        map[string]any `json:"-"`
}

// Event represents a Server-Sent Event received from GET /events.
type Event struct {
	Tag  string         `json:"tag"`
	Data map[string]any `json:"data"`
	Raw  string         `json:"raw,omitempty"`
}

var validKwargKeyRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ParseArgsAndKwargs splits CLI arguments into positional args and keyword args (key=value),
// mimicking Salt's native CLI argument parser.
func ParseArgsAndKwargs(rawArgs []string, rawStringsOnly bool) ([]any, map[string]any) {
	var args []any
	kwargs := make(map[string]any)

	for _, item := range rawArgs {
		idx := strings.IndexByte(item, '=')
		if idx > 0 {
			key := item[:idx]
			valStr := item[idx+1:]
			if validKwargKeyRe.MatchString(key) {
				if rawStringsOnly {
					kwargs[key] = valStr
				} else {
					kwargs[key] = CoerceScalarOrJSON(valStr)
				}
				continue
			}
		}
		if rawStringsOnly {
			args = append(args, item)
		} else {
			args = append(args, CoerceScalarOrJSON(item))
		}
	}

	if len(kwargs) == 0 {
		return args, nil
	}
	return args, kwargs
}

// CoerceScalarOrJSON converts Python/JSON-like literals ("True", "False", "None", numbers, JSON objects/arrays)
// into native Go values for salt-api JSON serialization.
func CoerceScalarOrJSON(s string) any {
	trimmed := strings.TrimSpace(s)
	switch trimmed {
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	case "null", "None", "none", "NULL":
		return nil
	}

	// Preserve leading-zero strings (like JIDs or octal modes "0644") as strings!
	if len(trimmed) > 1 && trimmed[0] == '0' && trimmed[1] >= '0' && trimmed[1] <= '9' {
		return s
	}

	if i, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f
	}

	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}

	return s
}
