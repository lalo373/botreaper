package skills

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Curator mirrors agent/curator.py: creates skills from experience and
// improves them during use. Agent-created skills archive under .archive/.
type Curator struct {
	home string
}

// NewCurator binds to a BOTREAPER_HOME.
func NewCurator(home string) *Curator { return &Curator{home: home} }

// Create writes .skills/<category>/<name>/SKILL.md from a Skill record.
func (c *Curator) Create(s types.Skill, category string) error {
	if s.Name == "" || category == "" {
		return errEmpty
	}
	dir := filepath.Join(c.home, ".skills", category, s.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := "---\nname: " + s.Name + "\ndescription: \"" + s.Description + "\"\nversion: " +
		nonEmpty(s.Version, "1.0.0") + "\nauthor: " + nonEmpty(s.Author, "botreaper") +
		"\nlicense: " + nonEmpty(s.License, "MIT") + "\n---\n\n# " + s.Name + " Skill\n\n" + s.Body + "\n"
	if s.CreatedBy == "agent" {
		body += "\ncreated_by: agent\ncreated_at: " + time.Now().UTC().Format(time.RFC3339) + "\n"
	}
	return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644)
}

// Archive moves agent-created skills to .archive/ (usage-gated).
func (c *Curator) Archive(category, name string) error {
	src := filepath.Join(c.home, ".skills", category, name)
	dst := filepath.Join(c.home, ".skills", ".archive", category, name+"-"+time.Now().UTC().Format("20060102"))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func nonEmpty(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

type errStr string

func (e errStr) Error() string { return string(e) }

const errEmpty errStr = "skills: empty name or category"
