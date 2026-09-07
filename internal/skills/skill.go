// Package skills implements skill discovery, lifecycle and curator.
// Ports skills/, optional-skills/, agent/curator.py, tools/skill_*.py.
package skills

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Frontmatter mirrors the SKILL.md contract (name/desc<=60ch, version,
// author, license, platforms, metadata.hermes{...}).
type Frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Author      string   `yaml:"author"`
	License     string   `yaml:"license"`
	Platforms   []string `yaml:"platforms"`
	Metadata    struct {
		BotReaper struct {
			Tags          []string       `yaml:"tags"`
			Category      string         `yaml:"category"`
			RelatedSkills []string       `yaml:"related_skills"`
			Config        map[string]any `yaml:"config"`
		} `yaml:"hermes"`
	} `yaml:"metadata"`
}

// Parse splits SKILL.md into frontmatter + body.
func Parse(raw string) (Frontmatter, string, error) {
	var fm Frontmatter
	if !strings.HasPrefix(raw, "---") {
		return fm, raw, nil
	}
	end := strings.Index(raw[3:], "---")
	if end < 0 {
		return fm, raw, nil
	}
	head := raw[3 : 3+end]
	body := raw[3+end+3:]
	if err := yaml.Unmarshal([]byte(head), &fm); err != nil {
		return fm, body, err
	}
	return fm, strings.TrimSpace(body), nil
}

// ToSkill converts parsed parts to a Skill record.
func ToSkill(fm Frontmatter, body, createdBy string) types.Skill {
	return types.Skill{
		Name: fm.Name, Description: fm.Description, Version: fm.Version,
		Author: fm.Author, License: fm.License, Platforms: fm.Platforms,
		Metadata: types.SkillMeta{Tags: fm.Metadata.BotReaper.Tags, Category: fm.Metadata.BotReaper.Category, RelatedSkills: fm.Metadata.BotReaper.RelatedSkills},
		Body:     body, CreatedBy: createdBy,
	}
}

// Discover walks roots for SKILL.md files (procedural discovery).
func Discover(roots ...string) []types.Skill {
	var out []types.Skill
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || info.Name() != "SKILL.md" {
				return nil
			}
			if strings.Contains(path, string(filepath.Separator)+".archive"+string(filepath.Separator)) {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			fm, body, err := Parse(string(raw))
			if err != nil || fm.Name == "" {
				return nil
			}
			out = append(out, ToSkill(fm, body, ""))
			return nil
		})
	}
	return out
}

// Describe renders the skill catalog line for the skills tool.
func Describe(skills []types.Skill) string {
	var sb strings.Builder
	for _, s := range skills {
		sb.WriteString("- " + s.Name + ": " + s.Description + "\n")
	}
	return sb.String()
}
