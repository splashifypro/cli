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

// mcpBinaryName is the splashify MCP server executable that OpenClaw launches.
const mcpBinaryName = "splashify-mcp"

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
  finish the Meta Embedded Signup, then run "splashify connect" again`)
		case "account_suspended":
			return fmt.Errorf("your account is not active — contact support to re-enable it")
		default:
			msg := elig.Message
			if msg == "" {
				msg = "your account is not eligible to use the CLI (reason: " + elig.Reason + ")"
			}
			return fmt.Errorf("%s", msg)
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
		return fmt.Errorf("usage: splashify link openclaw [--mcp-path <path>]")
	}
	fs := flag.NewFlagSet("link openclaw", flag.ContinueOnError)
	mcpPath := fs.String("mcp-path", "", "path to the "+mcpBinaryName+" binary")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}

	resolved, err := resolveMCPPath(cfg, *mcpPath)
	if err != nil {
		return err
	}
	// Remember the resolved path for future runs / doctor.
	if cfg.MCPPath != resolved {
		cfg.MCPPath = resolved
		_ = saveConfig(cfg)
	}

	addArgs := openclawAddArgs(cfg, resolved)

	openclaw, lookErr := exec.LookPath("openclaw")
	if lookErr != nil {
		fmt.Println("The `openclaw` CLI was not found on your PATH.")
		fmt.Println("Install it (npm install -g openclaw@latest), then run:")
		fmt.Println()
		fmt.Println("  openclaw " + strings.Join(addArgs, " "))
		return nil
	}

	fmt.Println("Registering the splashify MCP server with OpenClaw…")
	cmd := exec.Command(openclaw, addArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`openclaw mcp add` failed: %w", err)
	}
	fmt.Println()
	fmt.Println("✓ Linked. Restart the OpenClaw Gateway, then ask your assistant to")
	fmt.Println("  list your contacts or send a WhatsApp message to confirm.")
	return nil
}

// ─── mcp-config ──────────────────────────────────────────────────────────────

func cmdMCPConfig(_ []string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	resolved, _ := resolveMCPPath(cfg, "")
	if resolved == "" {
		resolved = "/path/to/" + mcpBinaryName
	}
	fmt.Println("Run this to register the splashify MCP server with OpenClaw:")
	fmt.Println()
	fmt.Println("  openclaw " + strings.Join(openclawAddArgs(cfg, resolved), " "))
	fmt.Println()
	fmt.Println("Environment passed to the MCP server:")
	fmt.Printf("  MCP_BACKEND_URL = %s\n", cfg.BaseURL)
	fmt.Printf("  MCP_AUTH_TOKEN  = %s…(redacted)\n", safePrefix(cfg.Token))
	fmt.Println("  MCP_APP_SCOPE   = true")
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
		check("openclaw CLI", true, "installed")
	} else {
		check("openclaw CLI", false, "not on PATH — npm install -g openclaw@latest")
	}

	mcp, err := resolveMCPPath(cfg, "")
	check(mcpBinaryName, err == nil, ternary(err == nil, mcp, "not found — build mcp/ or pass --mcp-path"))

	fmt.Println()
	if ok {
		fmt.Println("All checks passed.")
		return nil
	}
	return fmt.Errorf("some checks failed — see above")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// openclawAddArgs builds the argument list for `openclaw mcp add`.
func openclawAddArgs(cfg Config, mcpPath string) []string {
	return []string{
		"mcp", "add", "splashify",
		"--path", mcpPath,
		"--env", "MCP_BACKEND_URL=" + cfg.BaseURL,
		"--env", "MCP_AUTH_TOKEN=" + cfg.Token,
		"--env", "MCP_APP_SCOPE=true",
	}
}

// resolveMCPPath finds the splashify-mcp binary: explicit flag, saved config,
// PATH, then alongside the splashify executable.
func resolveMCPPath(cfg Config, override string) (string, error) {
	candidates := []string{override, cfg.MCPPath}
	for _, c := range candidates {
		if c != "" && fileExists(c) {
			return c, nil
		}
	}
	if p, err := exec.LookPath(mcpBinaryName); err == nil {
		return p, nil
	}
	if self, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(self), mcpBinaryName)
		if fileExists(sibling) {
			return sibling, nil
		}
	}
	return "", fmt.Errorf("%s not found — build it (cd mcp && go build -o %s ./cmd) and pass --mcp-path", mcpBinaryName, mcpBinaryName)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func safePrefix(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
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
