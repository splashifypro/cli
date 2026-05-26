package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The skill bundle must carry these three files. The names match the design
// doc (§5.1) and the user-facing install instructions.
var requiredSkillFiles = []string{"SKILL.md", "reference.md", "clawhub.json"}

func TestSkillBundleHasRequiredFiles(t *testing.T) {
	entries, err := fs.ReadDir(skillBundleFS, skillBundleRoot)
	if err != nil {
		t.Fatalf("read embedded bundle: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	for _, want := range requiredSkillFiles {
		if !got[want] {
			names := make([]string, 0, len(got))
			for n := range got {
				names = append(names, n)
			}
			sort.Strings(names)
			t.Errorf("embedded bundle missing %s — has %v", want, names)
		}
	}
}

func TestSkillMDFrontmatter(t *testing.T) {
	data, err := fs.ReadFile(skillBundleFS, skillBundleRoot+"/SKILL.md")
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	body := string(data)
	// Frontmatter is the YAML block at the very top, between two `---` lines.
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("SKILL.md does not start with YAML frontmatter")
	}
	// name: must equal skillName so the on-disk dir matches the manifest.
	if !strings.Contains(body, "name: "+skillName+"\n") {
		t.Errorf("SKILL.md frontmatter must declare `name: %s`", skillName)
	}
	// The skill is useless without the CLI — the bin requirement must be
	// declared so OpenClaw can refuse with a clean error if it is missing.
	if !strings.Contains(body, "bins: ["+skillName+"]") {
		t.Errorf("SKILL.md must declare metadata.openclaw.requires.bins: [%s]", skillName)
	}
}

func TestInstallSkillWritesEveryFile(t *testing.T) {
	skillsDir := t.TempDir()

	target, err := installSkill(skillsDir)
	if err != nil {
		t.Fatalf("installSkill: %v", err)
	}
	wantTarget := filepath.Join(skillsDir, skillName)
	if target != wantTarget {
		t.Errorf("installSkill returned %q, want %q", target, wantTarget)
	}

	for _, name := range requiredSkillFiles {
		dst := filepath.Join(target, name)
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("read installed %s: %v", name, err)
		}
		want, err := fs.ReadFile(skillBundleFS, skillBundleRoot+"/"+name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("installed %s differs from embedded copy", name)
		}
	}
}

func TestInstallSkillIsIdempotent(t *testing.T) {
	skillsDir := t.TempDir()

	first, err := installSkill(skillsDir)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	second, err := installSkill(skillsDir)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if first != second {
		t.Errorf("install target moved between runs: %q → %q", first, second)
	}

	// All embedded files should still match after re-install.
	for _, name := range requiredSkillFiles {
		got, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatalf("read %s after re-install: %v", name, err)
		}
		want, _ := fs.ReadFile(skillBundleFS, skillBundleRoot+"/"+name)
		if !bytes.Equal(got, want) {
			t.Errorf("re-installed %s differs from embedded copy", name)
		}
	}
}

func TestSkillInstalledDetectsBundle(t *testing.T) {
	skillsDir := t.TempDir()

	if skillInstalled(skillsDir) {
		t.Fatalf("skillInstalled true for empty dir")
	}
	if _, err := installSkill(skillsDir); err != nil {
		t.Fatalf("installSkill: %v", err)
	}
	if !skillInstalled(skillsDir) {
		t.Errorf("skillInstalled false after install")
	}
}

func TestResolveOpenclawSkillsDir(t *testing.T) {
	got, err := resolveOpenclawSkillsDir()
	if err != nil {
		t.Fatalf("resolveOpenclawSkillsDir: %v", err)
	}
	// Must end with the documented suffix on every OS. Using filepath.Join so
	// the test passes on both Windows ("\") and POSIX ("/").
	wantSuffix := filepath.Join(".openclaw", "workspace", "skills")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("resolveOpenclawSkillsDir = %q, want suffix %q", got, wantSuffix)
	}
}

func TestSkillTargetDirRespectsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-skills")
	got, err := skillTargetDir(override)
	if err != nil {
		t.Fatalf("skillTargetDir: %v", err)
	}
	want := filepath.Join(override, skillName)
	if got != want {
		t.Errorf("skillTargetDir(%q) = %q, want %q", override, got, want)
	}
}
