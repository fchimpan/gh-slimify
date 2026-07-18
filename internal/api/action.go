package api

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// ActionMetadata is the subset of an action.yml that determines how the
// action executes.
type ActionMetadata struct {
	Using string       // runs.using: "docker", "composite", "node20", ...
	Steps []ActionStep // runs.steps for composite actions
}

// ActionStep is a step of a composite action.
type ActionStep struct {
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
}

// ParseActionRef splits a remote uses: reference of the form
// "owner/repo[/path...][@ref]" into its components.
func ParseActionRef(uses string) (owner, repo, subpath, ref string, err error) {
	spec := uses
	if at := strings.LastIndex(uses, "@"); at >= 0 {
		spec, ref = uses[:at], uses[at+1:]
	}
	parts := strings.Split(spec, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", "", fmt.Errorf("invalid action reference: %s", uses)
	}
	owner = parts[0]
	repo = parts[1]
	subpath = strings.Join(parts[2:], "/")
	return owner, repo, subpath, ref, nil
}

// ParseActionMetadata parses the runs: section of an action.yml document.
func ParseActionMetadata(data []byte) (*ActionMetadata, error) {
	var raw struct {
		Runs struct {
			Using string       `yaml:"using"`
			Steps []ActionStep `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse action metadata: %w", err)
	}
	return &ActionMetadata{
		Using: raw.Runs.Using,
		Steps: raw.Runs.Steps,
	}, nil
}

// contentsResponse is the GitHub contents API response for a single file.
type contentsResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// GetActionMetadata fetches and parses the action.yml (or action.yaml) of an
// action from the GitHub contents API.
func (c *Client) GetActionMetadata(owner, repo, subpath, ref string) (*ActionMetadata, error) {
	var lastErr error
	for _, name := range []string{"action.yml", "action.yaml"} {
		filePath := name
		if subpath != "" {
			filePath = subpath + "/" + name
		}
		// Escape each path segment but keep the separators.
		escapedPath := (&url.URL{Path: filePath}).EscapedPath()
		apiPath := fmt.Sprintf("repos/%s/%s/contents/%s", owner, repo, escapedPath)
		if ref != "" {
			apiPath += "?ref=" + url.QueryEscape(ref)
		}

		var res contentsResponse
		if err := c.restClient.Get(apiPath, &res); err != nil {
			lastErr = err
			continue
		}
		if res.Encoding != "base64" {
			lastErr = fmt.Errorf("unexpected encoding %q for %s", res.Encoding, filePath)
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(res.Content, "\n", ""))
		if err != nil {
			lastErr = fmt.Errorf("failed to decode %s: %w", filePath, err)
			continue
		}
		return ParseActionMetadata(decoded)
	}
	return nil, fmt.Errorf("failed to fetch action metadata for %s/%s@%s: %w", owner, repo, ref, lastErr)
}
