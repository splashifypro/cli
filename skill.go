package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// skillBundleFS is the Splashify OpenClaw skill bundle, embedded at compile
// time so the CLI binary ships everything OpenClaw needs in a single file.
//
// Files appear under "openclaw-skill/…" inside the FS. Installation strips
// that prefix and writes into "<skillsDir>/splashify/".
//
//go:embed openclaw-skill/*
var skillBundleFS embed.FS

// skillBundleRoot is the directory name inside skillBundleFS that holds the
// bundle files. Must match the directory passed to //go:embed.
const skillBundleRoot = "openclaw-skill"

// skillName is the on-disk directory name OpenClaw expects under its skills
// root. It also matches the `name:` field in SKILL.md's frontmatter — both
// must stay in sync.
const skillName = "splashify"

// resolveOpenclawSkillsDir returns the path OpenClaw uses for workspace
// skills: <home>/.openclaw/workspace/skills. The caller is responsible for
// creating it if it does not exist; we only resolve.
func resolveOpenclawSkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw", "workspace", "skills"), nil
}

// skillTargetDir is the absolute path the bundle is installed to.
// If override is empty, it resolves OpenClaw's default skills root.
func skillTargetDir(override string) (string, error) {
	root := override
	if root == "" {
		r, err := resolveOpenclawSkillsDir()
		if err != nil {
			return "", err
		}
		root = r
	}
	return filepath.Join(root, skillName), nil
}

// installSkill writes the embedded bundle into <skillsDir>/splashify/.
// It is idempotent: existing files are overwritten so a re-install after a
// CLI upgrade picks up the latest instructions.
func installSkill(skillsDir string) (string, error) {
	target, err := skillTargetDir(skillsDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return target, fmt.Errorf("create %s: %w", target, err)
	}

	entries, err := fs.ReadDir(skillBundleFS, skillBundleRoot)
	if err != nil {
		return target, fmt.Errorf("read embedded bundle: %w", err)
	}
	if len(entries) == 0 {
		return target, fmt.Errorf("embedded bundle is empty — this is a build bug")
	}

	for _, e := range entries {
		if e.IsDir() {
			// The bundle is a flat directory today. If that ever changes,
			// teach this loop to recurse — for now, refuse silently.
			continue
		}
		src := filepath.ToSlash(filepath.Join(skillBundleRoot, e.Name()))
		data, err := fs.ReadFile(skillBundleFS, src)
		if err != nil {
			return target, fmt.Errorf("read embedded %s: %w", src, err)
		}
		dst := filepath.Join(target, e.Name())
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return target, fmt.Errorf("write %s: %w", dst, err)
		}
	}
	return target, nil
}

// skillInstalled reports whether the bundle's SKILL.md exists under the
// resolved (or overridden) OpenClaw skills directory. Used by `doctor`.
func skillInstalled(skillsDir string) bool {
	target, err := skillTargetDir(skillsDir)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(target, "SKILL.md"))
	return err == nil && !info.IsDir()
}
