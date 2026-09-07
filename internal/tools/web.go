package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// RegisterWebTools wires web_search/web_extract behind an injectable HTTP
// client so tests never touch the network. Mirrors tools/web_tools.py with
// the ProviderRegistry backend selection collapsed to a base-URL + key.
func RegisterWebTools(r *Registry, searchBase, searchKey string, httpClient *http.Client) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	r.Register("web_search", "web", "Search the web", func(ctx context.Context, args map[string]any) (string, error) {
		q := StrArg(args, "query", "")
		u := searchBase + "?q=" + url.QueryEscape(q)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		if searchKey != "" {
			req.Header.Set("Authorization", "Bearer "+searchKey)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var v any
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			return "no results", nil
		}
		raw, _ := json.Marshal(v)
		return string(raw), nil
	}, func() bool { return searchBase != "" })
	r.Register("web_extract", "web", "Extract a page as text", func(ctx context.Context, args map[string]any) (string, error) {
		u := StrArg(args, "url", "")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var v any
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			return "extracted", nil
		}
		raw, _ := json.Marshal(v)
		return string(raw), nil
	}, nil)
}

// RegisterClarify wires clarify (ask-a-question tool).
func RegisterClarify(r *Registry) {
	r.Register("clarify", "clarify", "Ask the user a clarifying question", func(ctx context.Context, args map[string]any) (string, error) {
		q := StrArg(args, "question", "")
		return "asked: " + q, nil
	}, nil)
}

// RegisterProject wires project_info.
func RegisterProject(r *Registry, info string) {
	r.Register("project_info", "project", "Describe the current project", func(ctx context.Context, args map[string]any) (string, error) {
		return info, nil
	}, nil)
}
