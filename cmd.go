package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

const defaultBaseURL = "https://api.splashifypro.com"

var stdin = bufio.NewReader(os.Stdin)

// prompt reads a line from stdin, showing an optional default.
func prompt(label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// ─── connect ─────────────────────────────────────────────────────────────────

func cmdConnect(args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	url := fs.String("url", "", "backend base URL")
	token := fs.String("token", "", "oc_live_ access token")
	if err := fs.Parse(args); err != nil {
		return err
	}

	baseURL := *url
	if baseURL == "" {
		baseURL = prompt("Backend URL", defaultBaseURL)
	}
	tok := *token
	if tok == "" {
		fmt.Println("Paste an access token from the app (Settings → Developer → Access Tokens).")
		tok = prompt("Access token", "")
	}
	tok = strings.TrimSpace(tok)
	if !strings.HasPrefix(tok, "oc_live_") {
		return fmt.Errorf("that does not look like an access token (expected oc_live_…)")
	}

	api := newAPIClient(baseURL, tok)

	// Validate before saving so a bad token fails loudly here, not later.
	email, name, err := api.me()
	if err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}

	// WABA gate — the CLI is only useful for accounts with a connected
	// WhatsApp Business Account. Refuse early with an actionable message so
	// the user is not left wondering why every later command fails.
	//
	// Escape hatch: SPLASHIFY_SKIP_ELIGIBILITY=1 skips the check entirely.
	// Useful while waiting on a backend rollout that fixes a server-side
	// gate bug, or for users connecting from a sub-account whose WABA lives
	// on a parent record. Token validation (the api.me() call above) still
	// runs — this only bypasses the WABA reachability check.
	if v := os.Getenv("SPLASHIFY_SKIP_ELIGIBILITY"); v != "" && v != "0" && v != "false" {
		fmt.Fprintln(os.Stderr, "note: SPLASHIFY_SKIP_ELIGIBILITY is set — skipping WABA eligibility check")
	} else {
		elig, err := api.cliEligibility()
		if err != nil {
			// If the backend is older and doesn't have the endpoint, fall through
			// rather than blocking — every shipped backend after sprint-2 has it.
			fmt.Fprintln(os.Stderr, "warning: could not check CLI eligibility —", err)
		} else if !elig.Eligible {
			switch elig.Reason {
			case "no_waba":
				return fmt.Errorf(`your Splashify Pro account does not have a WhatsApp Business Account connected yet.

  Open https://app.splashifypro.com → WhatsApp → Connect Number,
  finish the Meta Embedded Signup, then run "splashify connect" again.

  If you are certain the account already has a WABA and the gate is
  misreporting, retry with SPLASHIFY_SKIP_ELIGIBILITY=1 to bypass the check`)
			case "account_suspended":
				return fmt.Errorf("your account is not active — contact support to re-enable it")
			case "subscription_expired":
				url := elig.UpgradeURL
				if url == "" {
					url = "https://app.splashifypro.com/settings/subscriptions"
				}
				return fmt.Errorf(`your trial has ended and there is no active paid plan on this account.

  The splashify CLI requires an active subscription (paid plan OR
  unexpired trial).

  Upgrade your plan:  %s

  Run "splashify subscription" once a plan is active to confirm,
  then re-run "splashify connect".`, url)
			default:
				msg := elig.Message
				if msg == "" {
					msg = "your account is not eligible to use the CLI (reason: " + elig.Reason + ")"
				}
				return fmt.Errorf("%s", msg)
			}
		}
	}

	cfg, _ := loadConfig()
	cfg.BaseURL = strings.TrimRight(baseURL, "/")
	cfg.Token = tok
	if err := saveConfig(cfg); err != nil {
		return err
	}

	who := email
	if name != "" {
		who = fmt.Sprintf("%s (%s)", name, email)
	}
	fmt.Printf("✓ Connected as %s\n", who)
	fmt.Println("  Next: run `splashify link openclaw` to wire this account into OpenClaw.")
	return nil
}

// ─── whoami ──────────────────────────────────────────────────────────────────

func cmdWhoami(_ []string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	email, name, err := newAPIClient(cfg.BaseURL, cfg.Token).me()
	if err != nil {
		return err
	}
	fmt.Printf("Account : %s\n", email)
	if name != "" {
		fmt.Printf("Name    : %s\n", name)
	}
	fmt.Printf("Backend : %s\n", cfg.BaseURL)
	return nil
}

// ─── token ───────────────────────────────────────────────────────────────────

func cmdToken(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify token <list|create|revoke>")
	}
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	switch args[0] {
	case "list":
		tokens, err := api.listTokens()
		if err != nil {
			return err
		}
		if len(tokens) == 0 {
			fmt.Println("No access tokens.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tPREFIX\tLAST USED\tSTATUS")
		for _, t := range tokens {
			status := "active"
			if t.Revoked {
				status = "revoked"
			}
			lastUsed := t.LastUsedAt
			if lastUsed == "" || strings.HasPrefix(lastUsed, "0001-") {
				lastUsed = "never"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Name, t.TokenPrefix, lastUsed, status)
		}
		return w.Flush()

	case "create":
		fs := flag.NewFlagSet("token create", flag.ContinueOnError)
		name := fs.String("name", "", "label for the token")
		expires := fs.Int("expires-days", 0, "days until expiry (0 = never)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			*name = prompt("Token name", "OpenClaw")
		}
		id, raw, err := api.createToken(*name, *expires)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Created token %s\n\n", id)
		fmt.Printf("    %s\n\n", raw)
		fmt.Println("Copy it now — it will not be shown again.")
		return nil

	case "revoke":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify token revoke <id>")
		}
		if err := api.revokeToken(args[1]); err != nil {
			return err
		}
		fmt.Printf("✓ Revoked token %s\n", args[1])
		return nil

	default:
		return fmt.Errorf("unknown token subcommand: %s", args[0])
	}
}

// ─── link ────────────────────────────────────────────────────────────────────

func cmdLink(args []string) error {
	if len(args) == 0 || args[0] != "openclaw" {
		return fmt.Errorf("usage: splashify link openclaw [--path <skills-dir>] [--print]")
	}
	fs := flag.NewFlagSet("link openclaw", flag.ContinueOnError)
	path := fs.String("path", "", "OpenClaw skills directory to install into (defaults to ~/.openclaw/workspace/skills)")
	print := fs.Bool("print", false, "print the target path without writing anything")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	target, err := skillTargetDir(*path)
	if err != nil {
		return err
	}

	// --print is a pure path-resolver — let it run before connect so a user
	// can preview where the bundle would land without first authenticating.
	if *print {
		fmt.Println(target)
		return nil
	}

	// Connection is not strictly required to drop the bundle on disk, but the
	// skill is useless without it — fail early with the same message connect
	// errors give so users do not get a stale skill installed for no account.
	if _, err := requireConfig(); err != nil {
		return err
	}

	if *path == "" {
		// Surface a clearer error than "permission denied on …/skills" when
		// OpenClaw has not been onboarded yet (the parent ~/.openclaw is
		// missing). We still try to install — if the dir tree is creatable we
		// proceed silently.
		root, _ := resolveOpenclawSkillsDir()
		parent := filepath.Dir(root) // ~/.openclaw/workspace
		grand := filepath.Dir(parent)  // ~/.openclaw
		if !fileDirExists(grand) {
			fmt.Fprintln(os.Stderr, "note: OpenClaw is not onboarded yet —")
			fmt.Fprintln(os.Stderr, "      install it with `npm install -g openclaw@latest`")
			fmt.Fprintln(os.Stderr, "      and run `openclaw onboard --install-daemon`, or")
			fmt.Fprintln(os.Stderr, "      pass --path <skills-dir> to install elsewhere.")
		}
	}

	installed, err := installSkill(*path)
	if err != nil {
		return err
	}

	fmt.Printf("✓ Splashify skill installed at %s\n", installed)
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  1. Restart OpenClaw so it picks up the skill (`openclaw dashboard`).")
	fmt.Println("  2. Ask your assistant something simple — \"list my Splashify contacts\".")
	return nil
}

// ─── doctor ──────────────────────────────────────────────────────────────────

func cmdDoctor(_ []string) error {
	ok := true
	check := func(label string, pass bool, detail string) {
		mark := "✓"
		if !pass {
			mark = "✗"
			ok = false
		}
		fmt.Printf("  %s %s — %s\n", mark, label, detail)
	}

	cfg, _ := loadConfig()
	check("config", cfg.Token != "" && cfg.BaseURL != "",
		ternary(cfg.Token != "", "found", "missing — run `splashify connect`"))

	if cfg.Token != "" && cfg.BaseURL != "" {
		api := newAPIClient(cfg.BaseURL, cfg.Token)
		email, _, err := api.me()
		check("access token", err == nil, ternary(err == nil, "valid ("+email+")", errStr(err)))

		// WABA status — informational, not a hard failure on its own. The CLI
		// keeps working for non-WABA endpoints; the user only sees this as a
		// warning until they connect a number.
		if err == nil {
			if elig, eligErr := api.cliEligibility(); eligErr == nil {
				if elig.WabaConnected {
					check("waba", true, "connected")
				} else {
					check("waba", false, "not connected — finish Meta Embedded Signup at app.splashifypro.com")
				}
			}
		}
	}

	if _, err := exec.LookPath("openclaw"); err == nil {
		check("openclaw", true, "installed")
	} else {
		check("openclaw", false, "not on PATH — npm install -g openclaw@latest")
	}

	if skillInstalled("") {
		target, _ := skillTargetDir("")
		check("splashify skill", true, "installed at "+target)
	} else {
		check("splashify skill", false, "not installed — run `splashify link openclaw`")
	}

	fmt.Println()
	if ok {
		fmt.Println("All checks passed.")
		return nil
	}
	return fmt.Errorf("some checks failed — see above")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// fileDirExists reports whether path exists and is a directory.
func fileDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
