package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// updateCheckRepo is the GitHub repo whose Releases drive the update check.
// The release tag pattern is `vX.Y.Z` — see .goreleaser.yaml in that repo.
const updateCheckRepo = "splashifypro/cli"

// updateResult carries what the post-run printer needs. A nil pointer means
// the check was skipped or failed silently.
type updateResult struct {
	latest      string // tag name with the `cli/` prefix stripped, e.g. "v0.2.0"
	installLine string
}

// startUpdateCheck kicks off an async fetch of the latest release tag. It
// returns a channel that yields at most one *updateResult.
//
// Skipped (returns nil channel) when:
//   - version is "dev" (developer build — irrelevant to update)
//   - SPLASHIFY_NO_UPDATE_CHECK is set to a non-empty value
//   - the command is itself `version` / `--version` / `-v` (avoids noise on
//     the one command whose entire purpose is showing the version)
func startUpdateCheck() <-chan *updateResult {
	if version == "dev" {
		return nil
	}
	if v := os.Getenv("SPLASHIFY_NO_UPDATE_CHECK"); v != "" && v != "0" && v != "false" {
		return nil
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			return nil
		}
	}

	out := make(chan *updateResult, 1)
	go func() {
		defer close(out)
		latest, ok := fetchLatestRelease(2 * time.Second)
		if !ok {
			return
		}
		if !isNewerVersion(latest, version) {
			return
		}
		out <- &updateResult{
			latest:      latest,
			installLine: defaultInstallCommand(),
		}
	}()
	return out
}

// finishUpdateCheck waits a short, bounded time for the update check to
// finish and prints a one-line hint to stderr if a newer version exists.
// Never blocks the user noticeably.
func finishUpdateCheck(ch <-chan *updateResult) {
	if ch == nil {
		return
	}
	select {
	case res, ok := <-ch:
		if !ok || res == nil {
			return
		}
		fmt.Fprintf(os.Stderr,
			"\nA newer splashify is available (%s → %s).\nUpdate: %s\n",
			version, res.latest, res.installLine,
		)
	case <-time.After(500 * time.Millisecond):
		// Network was slow; give up rather than block the user. The next
		// invocation will get another chance.
	}
}

// fetchLatestRelease asks GitHub for the latest CLI release. The dedicated
// splashifypro/cli repo only carries CLI tags, so /releases/latest is exactly
// what we want — no per-tag filtering needed.
func fetchLatestRelease(timeout time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := "https://api.github.com/repos/" + updateCheckRepo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "splashify-cli/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", false
	}

	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", false
	}
	if release.Draft || release.Prerelease {
		return "", false
	}
	if !strings.HasPrefix(release.TagName, "v") {
		return "", false
	}
	return release.TagName, true
}

// isNewerVersion reports whether `latest` is strictly greater than `current`
// using a naïve dotted-int compare on the "vMAJOR.MINOR.PATCH" prefix. Any
// suffix (e.g. "-rc1") is ignored — pre-releases never trigger the hint.
func isNewerVersion(latest, current string) bool {
	la := parseSemver(latest)
	cu := parseSemver(current)
	for i := 0; i < 3; i++ {
		if la[i] != cu[i] {
			return la[i] > cu[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		n := 0
		for _, c := range parts[i] {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out[i] = n
	}
	return out
}

// defaultInstallCommand returns the platform-appropriate one-liner shown in
// the update hint. The install scripts live at the repo root.
func defaultInstallCommand() string {
	const base = "https://raw.githubusercontent.com/" + updateCheckRepo + "/main/"
	if runtime.GOOS == "windows" {
		return "iwr -useb " + base + "install.ps1 | iex"
	}
	return "curl -fsSL " + base + "install.sh | sh"
}
