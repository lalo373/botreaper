package skills

import (
	"testing"

	"github.com/nousresearch/botreaper/pkg/types"
)

const sampleSKILL = `---
name: arxiv
description: "Search arXiv papers."
version: 1.0.0
author: tester
license: MIT
platforms: [linux]
metadata:
  hermes:
    tags: [research]
    category: research
    related_skills: [pdf]
---
# Arxiv Skill

Use terminal to query.
`

func TestParseSkill(t *testing.T) {
	fm, body, err := Parse(sampleSKILL)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Name != "arxiv" || len(fm.Platforms) != 1 {
		t.Fatalf("fm = %+v", fm)
	}
	if body == "" {
		t.Fatal("empty body")
	}
	s := ToSkill(fm, body, "agent")
	if s.Metadata.Category != "research" {
		t.Fatalf("skill = %+v", s)
	}
}

func TestCuratorCreateArchive(t *testing.T) {
	home := t.TempDir()
	c := NewCurator(home)
	s := types.Skill{Name: "demo", Description: "Demo skill.", Body: "Do things."}
	if err := c.Create(s, "research"); err != nil {
		t.Fatal(err)
	}
	if got := Discover(home + "/.skills"); len(got) != 1 {
		t.Fatalf("discovered = %d", len(got))
	}
	if err := c.Archive("research", "demo"); err != nil {
		t.Fatal(err)
	}
	if got := Discover(home + "/.skills"); len(got) != 0 {
		t.Fatalf("archived skill still discovered: %d", len(got))
	}
}
