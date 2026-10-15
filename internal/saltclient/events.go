package saltclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
)

// StreamEvents connects to GET /events (Server-Sent Events) and invokes handler for each event.
// Optional tagPattern (supporting filepath.Match / glob like "salt/job/*") filters incoming events.
func (c *Client) StreamEvents(ctx context.Context, tagPattern string, maxEvents int, handler func(Event) error) error {
	token, err := c.EnsureToken(ctx, false)
	if err != nil {
		return err
	}

	url := c.profile.URL + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Auth-Token", token)

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("GET /events failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET /events returned HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	var currentTag string
	count := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "tag:") {
			currentTag = strings.TrimSpace(strings.TrimPrefix(line, "tag:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			rawJSON := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if rawJSON == "" {
				continue
			}

			var ev Event
			ev.Raw = rawJSON
			if err := json.Unmarshal([]byte(rawJSON), &ev); err != nil {
				ev.Tag = currentTag
				ev.Data = map[string]any{"raw": rawJSON}
			}
			if ev.Tag == "" {
				ev.Tag = currentTag
			}

			if tagPattern != "" && !matchEventTag(tagPattern, ev.Tag) {
				currentTag = ""
				continue
			}

			if err := handler(ev); err != nil {
				return err
			}
			currentTag = ""
			count++
			if maxEvents > 0 && count >= maxEvents {
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("error reading event stream: %w", err)
	}
	return nil
}

// SendWebhook triggers a Salt NetAPI webhook via POST /hook/<tag>.
func (c *Client) SendWebhook(ctx context.Context, hookTag string, payload map[string]any) (any, error) {
	hookTag = strings.Trim(hookTag, "/")
	if hookTag == "" {
		return nil, fmt.Errorf("webhook tag cannot be empty")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	var out any
	_, err := c.doJSON(ctx, http.MethodPost, "/hook/"+hookTag, payload, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func matchEventTag(pattern, tag string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	matched, err := path.Match(pattern, tag)
	if err == nil && matched {
		return true
	}
	return strings.Contains(tag, pattern)
}
