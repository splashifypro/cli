package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// This file implements `splashify devices` (aliases: sessions, ses) and
// `splashify session <id>` (alias: device) — the CLI mirror of the app's
// /settings/devices page. It covers:
//
//	list — every active login session on the account
//	get  — show one session by id (list-and-filter)
//	logout / revoke — remotely sign out a specific session
//	logout-all — sign out every session in one go
//
// All commands hit /api/v1/app/auth/sessions[/:id]. The page only ever lists
// + revokes; we keep the CLI surface tight to match.
//
// `is_current` on each row is set by the backend by comparing the row's
// session_id with c.Locals("app_session_id"). When the CLI authenticates
// with an oc_live_ access token there is no session local set, so every row
// reports is_current: false from the CLI — by design.

// ─── splashify devices ───────────────────────────────────────────────────────

func cmdDevices(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return cmdDevicesList(args)

	case "logout", "revoke", "signout", "sign-out":
		return cmdDevicesLogout(args[1:])

	case "logout-all", "revoke-all", "signout-all", "sign-out-all":
		return cmdDevicesLogoutAll(args[1:])

	default:
		return fmt.Errorf("unknown devices subcommand: %s\nrun: splashify devices", sub)
	}
}

// cmdDevicesList implements `splashify devices` / `splashify devices list`,
// with two optional client-side filters that mirror what users typically
// reach for in the page's search box: by platform and by IP.
func cmdDevicesList(args []string) error {
	fs := flag.NewFlagSet("devices list", flag.ContinueOnError)
	platform := fs.String("platform", "", "filter by platform: web | android | ios | mobile (client-side)")
	ip := fs.String("ip", "", "filter by IP address (client-side; exact match)")

	// Strip the leading "list"/"ls" subcommand if present.
	flagArgs := args
	if len(args) > 0 && (args[0] == "list" || args[0] == "ls" || args[0] == "") {
		flagArgs = args[1:]
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	if *platform == "" && *ip == "" {
		return runReq("GET", "/app/auth/sessions", nil)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/auth/sessions", nil)
	if err != nil {
		return err
	}

	var list struct {
		Success  bool             `json:"success"`
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}

	wantPlatform := strings.ToLower(strings.TrimSpace(*platform))
	wantIP := strings.TrimSpace(*ip)
	matched := make([]map[string]any, 0, len(list.Sessions))
	for _, s := range list.Sessions {
		if wantPlatform != "" {
			p, _ := s["platform"].(string)
			if strings.ToLower(p) != wantPlatform {
				continue
			}
		}
		if wantIP != "" {
			ipa, _ := s["ip_address"].(string)
			if ipa != wantIP {
				continue
			}
		}
		matched = append(matched, s)
	}
	out := map[string]any{"success": true, "sessions": matched, "count": len(matched)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

// cmdDevicesLogout revokes a single session by id.
func cmdDevicesLogout(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify devices logout <session_id>")
	}
	return runReq("DELETE", "/app/auth/sessions/"+args[0], nil)
}

// cmdDevicesLogoutAll sign-outs every active session. The backend has no
// bulk-revoke endpoint, so this iterates the list and fires one DELETE per
// session. The CLI's own access (oc_live_ token) is unaffected — tokens are
// stored in a separate table, so you can safely "log out everywhere" from
// the CLI without losing CLI access.
//
// Flags:
//
//	--yes     skip the confirmation prompt (for scripts)
//	--platform <p>   only revoke sessions matching this platform
//	--ip <addr>      only revoke sessions matching this IP
func cmdDevicesLogoutAll(args []string) error {
	fs := flag.NewFlagSet("devices logout-all", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	platform := fs.String("platform", "", "only revoke sessions on this platform (web | android | ios | mobile)")
	ip := fs.String("ip", "", "only revoke sessions on this IP")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	raw, err := api.callRaw("GET", "/app/auth/sessions", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success  bool             `json:"success"`
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("decode sessions list: %w", err)
	}

	wantPlatform := strings.ToLower(strings.TrimSpace(*platform))
	wantIP := strings.TrimSpace(*ip)

	targets := make([]map[string]any, 0, len(list.Sessions))
	for _, s := range list.Sessions {
		if wantPlatform != "" {
			p, _ := s["platform"].(string)
			if strings.ToLower(p) != wantPlatform {
				continue
			}
		}
		if wantIP != "" {
			ipa, _ := s["ip_address"].(string)
			if ipa != wantIP {
				continue
			}
		}
		targets = append(targets, s)
	}

	if len(targets) == 0 {
		fmt.Println("No matching sessions to revoke.")
		return nil
	}

	if !*yes {
		fmt.Printf("About to revoke %d session(s):\n", len(targets))
		for _, s := range targets {
			sid, _ := s["session_id"].(string)
			p, _ := s["platform"].(string)
			ipa, _ := s["ip_address"].(string)
			dev, _ := s["device_info"].(string)
			fmt.Printf("  %s  %s  %s  %s\n", sid, p, ipa, dev)
		}
		ans := prompt("Continue? (yes/no)", "no")
		if !strings.EqualFold(ans, "yes") && !strings.EqualFold(ans, "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	revoked := 0
	failed := 0
	for _, s := range targets {
		sid, _ := s["session_id"].(string)
		if sid == "" {
			continue
		}
		if err := api.do("DELETE", "/app/auth/sessions/"+sid, nil, nil); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s — %s\n", sid, err.Error())
			failed++
			continue
		}
		fmt.Printf("  ✓ %s\n", sid)
		revoked++
	}
	fmt.Printf("\n%d revoked, %d failed.\n", revoked, failed)
	if failed > 0 {
		return fmt.Errorf("%d revocations failed", failed)
	}
	return nil
}

// ─── splashify session / device <id> ─────────────────────────────────────────

// cmdSession shows one session record. There is no per-id GET on the
// backend, so we list-and-filter — same pattern as `splashify member <id>`,
// `splashify canned <id>`, and `splashify instagram rule <id>`.
func cmdSession(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify session <session_id>")
	}
	id := args[0]

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/auth/sessions", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success  bool             `json:"success"`
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		printJSON(raw)
		return nil
	}
	for _, s := range list.Sessions {
		if sid, _ := s["session_id"].(string); sid == id {
			encoded, _ := json.MarshalIndent(s, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("session %s not found", id)
}
