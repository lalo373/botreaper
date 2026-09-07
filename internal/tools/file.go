package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// RegisterFileTools wires read_file/write_file/patch/search_files.
// Mirrors tools/file_tools.py semantics.
func RegisterFileTools(r *Registry, cwd string) {
	r.Register("read_file", "file", "Read a file fully", func(ctx context.Context, args map[string]any) (string, error) {
		p := resolve(cwd, StrArg(args, "path", ""))
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}, nil)
	r.Register("write_file", "file", "Write a file atomically", func(ctx context.Context, args map[string]any) (string, error) {
		p := resolve(cwd, StrArg(args, "path", ""))
		content := StrArg(args, "content", "")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		tmp, err := os.CreateTemp(filepath.Dir(p), ".write-*.tmp")
		if err != nil {
			return "", err
		}
		if _, err := tmp.WriteString(content); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return "", err
		}
		_ = tmp.Close()
		if err := os.Rename(tmp.Name(), p); err != nil {
			_ = os.Remove(tmp.Name())
			return "", err
		}
		return "ok", nil
	}, nil)
	r.Register("patch", "file", "Apply a search/replace patch", func(ctx context.Context, args map[string]any) (string, error) {
		p := resolve(cwd, StrArg(args, "path", ""))
		search := StrArg(args, "search", "")
		replace := StrArg(args, "replace", "")
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		if !strings.Contains(string(raw), search) {
			return "", errNoMatch
		}
		out := strings.Replace(string(raw), search, replace, 1)
		if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
			return "", err
		}
		return "ok", nil
	}, nil)
	r.Register("search_files", "file", "Search files for text", func(ctx context.Context, args map[string]any) (string, error) {
		root := resolve(cwd, StrArg(args, "root", "."))
		needle := StrArg(args, "query", "")
		hits := []string{}
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || len(hits) >= 50 {
				return nil
			}
			if info.Size() > 1<<20 {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if strings.Contains(string(raw), needle) {
				rel, _ := filepath.Rel(root, path)
				hits = append(hits, rel)
			}
			return nil
		})
		raw, _ := json.Marshal(hits)
		return string(raw), nil
	}, nil)
}

func resolve(cwd, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}

var errNoMatch = errString("patch: search block not found")

type errString string

func (e errString) Error() string { return string(e) }
