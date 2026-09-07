package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// promptPool reuses prompt builder buffers (zero-alloc turn assembly).
var promptPool = sync.Pool{New: func() any { sb := new(strings.Builder); sb.Grow(16 * 1024); return sb }}

// systemPromptSnapshot is immutable per session; BuildPrompt appends only
// the mutable tail (transcript + CWD context capped), preserving KV-cache.
func (a *AIAgent) systemSnapshot() string { return a.opts.SystemPrompt }

// BuildPrompt assembles the API messages: frozen system prefix + history.
// CWD context is capped (mirrors prompt_builder.py) and subdirectory hints
// are appended to tool results, not the prefix.
func BuildPrompt(system string, history []string, cwd string) string {
	sb := promptPool.Get().(*strings.Builder)
	defer func() { sb.Reset(); promptPool.Put(sb) }()
	sb.WriteString(system)
	if cwd != "" {
		sb.WriteString("\n\n[CWD]\n")
		sb.WriteString(cwd)
		sb.WriteString("\n")
		if entries := cwdListing(cwd); entries != "" {
			sb.WriteString(entries)
		}
	}
	for _, h := range history {
		sb.WriteString("\n")
		sb.WriteString(h)
	}
	return sb.String()
}

func cwdListing(cwd string) string {
	entries, err := os.ReadDir(cwd)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[ls] ")
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if n > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(e.Name())
		n++
		if n >= 40 || sb.Len() > 2000 {
			break
		}
	}
	if p, err := filepath.Abs(cwd); err == nil && p != cwd {
		sb.WriteString(" (" + p + ")")
	}
	return sb.String()
}
