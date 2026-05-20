package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify team` / `splashify member` — the CLI mirror of
// the app's Settings → Agents page. It covers the full lifecycle:
//
//	list / get / add (with inline OTP verify) / update / set-role / set-permissions
//	delete / verify / resend-otp / resend-invite
//
// All commands hit /api/v1/app/team/* with the stored oc_live_ token.

// teamPageKeys are the permission keys the app surfaces in
// app/(routes)/settings/agents/components/CreateAgentDialog.tsx → PAGE_GROUPS.
// We keep them in CLI-side lockstep so that `--all`, `--rw`, `--read`, `--none`
// can build a complete permissions map without the caller listing every key.
//
// The map is permissive on the backend — it stores whatever keys you send — so
// `--permissions '{"custom_key":"read"}'` keeps working even if it isn't in
// this list. The known list is only the substrate for the bulk-level flags.
var teamPageKeys = []string{
	"dashboard", "messages", "contacts",
	"broadcasts", "templates", "media",
	"analytics", "ai-agents", "ai-credits",
	"integrations", "wallet", "settings", "activity-logs",
}

// teamPermissionLevels are the three values the app uses per page key.
// "none" / "read" / "read_write" — same values stored in DB.
var teamPermissionLevels = map[string]bool{
	"none": true, "read": true, "read_write": true,
}

// teamRoles are the valid role values the backend accepts (anything else
// silently coerces to "agent" on create, but is rejected on update — so we
// validate here for a consistent error before the round-trip).
var teamRoles = map[string]bool{"agent": true, "manager": true}

// ─── splashify team ──────────────────────────────────────────────────────────

func cmdTeam(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls", "members":
		return runReq("GET", "/app/team", nil)

	case "add", "create", "invite":
		return cmdTeamAdd(args[1:])

	case "verify":
		return cmdTeamVerify(args[1:])

	case "resend-otp":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify team resend-otp <member_id>")
		}
		return runReq("POST", "/app/team/resend-otps", map[string]any{
			"member_id": args[1],
		})

	case "resend-invite", "resend-password-email":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify team resend-invite <member_id>")
		}
		return runReq("POST", "/app/team/"+args[1]+"/resend-password-email", nil)

	case "update", "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify team update <member_id> [--role …] [--permissions … | --all …]")
		}
		return cmdTeamUpdate(args[1], args[2:])

	case "set-role":
		if len(args) < 3 {
			return fmt.Errorf("usage: splashify team set-role <member_id> <agent|manager>")
		}
		role := strings.ToLower(strings.TrimSpace(args[2]))
		if !teamRoles[role] {
			return fmt.Errorf("role must be 'agent' or 'manager', got %q", args[2])
		}
		return runReq("PUT", "/app/team/"+args[1], map[string]any{"role": role})

	case "set-permissions", "permissions":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify team set-permissions <member_id> [--permissions … | --all … | --rw … | --read … | --none …]")
		}
		return cmdTeamSetPermissions(args[1], args[2:])

	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify team delete <member_id>")
		}
		return runReq("DELETE", "/app/team/"+args[1], nil)

	default:
		return fmt.Errorf("unknown team subcommand: %s\nrun: splashify team", sub)
	}
}

// cmdMember implements the singular `splashify member <id>` — convenience for
// "show one member", since the backend has no per-id GET. Falls back to the
// list-and-filter pattern used elsewhere (e.g. `splashify attribute <id>`).
func cmdMember(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify member <id>")
	}
	id := args[0]

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", "/app/team", nil)
	if err != nil {
		return err
	}
	var list struct {
		Success bool             `json:"success"`
		Members []map[string]any `json:"members"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		// Couldn't parse — surface the raw response for debugging.
		printJSON(raw)
		return nil
	}
	for _, m := range list.Members {
		if mid, _ := m["member_id"].(string); mid == id {
			encoded, _ := json.MarshalIndent(m, "", "  ")
			fmt.Println(string(encoded))
			return nil
		}
	}
	return fmt.Errorf("team member %s not found", id)
}

// ─── add ─────────────────────────────────────────────────────────────────────

// cmdTeamAdd creates a team member and, by default, prompts for the OTP that
// the backend sends to the OWNER's email + WhatsApp — wrapping the two-step
// CreateMember → VerifyMemberOTPs flow into one command.
//
// Flags:
//
//	--name          required, the team member's display name
//	--email         required
//	--country-code  required, e.g. +91
//	--phone         required, digits only (no country code)
//	--role          agent (default) | manager
//	--permissions   JSON map, e.g. '{"messages":"read_write"}'
//	--all           shortcut: set every known page to this level
//	--rw            comma-separated page keys to grant read_write
//	--read          comma-separated page keys to grant read
//	--none          comma-separated page keys to grant none
//	--otp           OTP to verify with immediately (skips prompt)
//	--no-verify     skip the verification step entirely
func cmdTeamAdd(args []string) error {
	fs := flag.NewFlagSet("team add", flag.ContinueOnError)
	name := fs.String("name", "", "display name (required)")
	email := fs.String("email", "", "contact email (required)")
	cc := fs.String("country-code", "", "country code, e.g. +91 (required)")
	phone := fs.String("phone", "", "phone digits, no country code (required)")
	role := fs.String("role", "agent", "role: agent | manager")

	permsJSON := fs.String("permissions", "", `permission map JSON, e.g. '{"messages":"read_write"}'`)
	allLevel := fs.String("all", "", "set every known page to this level: none | read | read_write")
	rwKeys := fs.String("rw", "", "comma-separated page keys to grant read_write")
	readKeys := fs.String("read", "", "comma-separated page keys to grant read")
	noneKeys := fs.String("none", "", "comma-separated page keys to grant none")

	otp := fs.String("otp", "", "OTP to verify with immediately (skips the prompt)")
	noVerify := fs.Bool("no-verify", false, "create only; do not prompt to verify the OTP")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *email == "" || *cc == "" || *phone == "" {
		return fmt.Errorf("usage: splashify team add --name … --email … --country-code +91 --phone 9876543210 [--role agent|manager] [--all read_write | --permissions '{…}']")
	}
	if !teamRoles[strings.ToLower(*role)] {
		return fmt.Errorf("--role must be 'agent' or 'manager', got %q", *role)
	}

	permissions, err := buildTeamPermissions(*permsJSON, *allLevel, *rwKeys, *readKeys, *noneKeys)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":         *name,
		"email":        *email,
		"country_code": *cc,
		"phone":        *phone,
		"role":         strings.ToLower(*role),
	}
	if permissions != nil {
		body["permissions"] = permissions
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	raw, err := api.callRaw("POST", "/app/team", body)
	if err != nil {
		return err
	}

	var created struct {
		Success  bool   `json:"success"`
		Message  string `json:"message"`
		MemberID string `json:"member_id"`
	}
	_ = json.Unmarshal(raw, &created)

	printJSON(raw)

	if !created.Success || created.MemberID == "" {
		// The backend reported failure or an unexpected shape — already printed,
		// just return so the exit code is non-zero.
		return fmt.Errorf("create failed (no member_id returned)")
	}

	if *noVerify {
		fmt.Println()
		fmt.Println("Created. An OTP has been sent to YOUR email and WhatsApp.")
		fmt.Printf("Next: splashify team verify %s --otp <code>\n", created.MemberID)
		return nil
	}

	// Verify inline. If --otp was given, use it directly; otherwise prompt.
	code := strings.TrimSpace(*otp)
	if code == "" {
		fmt.Println()
		fmt.Println("An OTP has been sent to YOUR email and WhatsApp.")
		fmt.Println("Enter it to finish the invite, or press Enter to skip (you can run")
		fmt.Printf("  splashify team verify %s --otp <code>\nlater).\n", created.MemberID)
		code = prompt("OTP", "")
		if code == "" {
			fmt.Println("Skipped verification.")
			return nil
		}
	}

	verifyRaw, err := api.callRaw("POST", "/app/team/verify-otps", map[string]any{
		"member_id": created.MemberID,
		"otp":       code,
	})
	if err != nil {
		return err
	}
	fmt.Println()
	printJSON(verifyRaw)
	return nil
}

// ─── verify ──────────────────────────────────────────────────────────────────

func cmdTeamVerify(args []string) error {
	fs := flag.NewFlagSet("team verify", flag.ContinueOnError)
	otp := fs.String("otp", "", "the OTP sent to your email + WhatsApp")
	// member_id is positional; everything after may be flags
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		positional = append(positional, a)
	}
	if len(positional) < 1 {
		return fmt.Errorf("usage: splashify team verify <member_id> --otp <code>")
	}
	if err := fs.Parse(args[len(positional):]); err != nil {
		return err
	}
	if *otp == "" {
		*otp = prompt("OTP", "")
	}
	if *otp == "" {
		return fmt.Errorf("--otp is required")
	}
	return runReq("POST", "/app/team/verify-otps", map[string]any{
		"member_id": positional[0],
		"otp":       strings.TrimSpace(*otp),
	})
}

// ─── update / set-permissions ────────────────────────────────────────────────

// cmdTeamUpdate is the generic PUT — accepts --role and/or any of the
// permission flags. The backend treats an absent field as "leave alone", so
// we only include keys the user explicitly set.
func cmdTeamUpdate(memberID string, args []string) error {
	fs := flag.NewFlagSet("team update", flag.ContinueOnError)
	role := fs.String("role", "", "new role: agent | manager")

	permsJSON := fs.String("permissions", "", `permission map JSON, e.g. '{"messages":"read_write"}'`)
	allLevel := fs.String("all", "", "set every known page to this level: none | read | read_write")
	rwKeys := fs.String("rw", "", "comma-separated page keys to grant read_write")
	readKeys := fs.String("read", "", "comma-separated page keys to grant read")
	noneKeys := fs.String("none", "", "comma-separated page keys to grant none")

	if err := fs.Parse(args); err != nil {
		return err
	}

	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify team update <member_id> [--role agent|manager] [--all level | --permissions '{…}' | --rw keys | --read keys | --none keys]")
	}

	body := map[string]any{}
	if passed["role"] {
		r := strings.ToLower(strings.TrimSpace(*role))
		if !teamRoles[r] {
			return fmt.Errorf("--role must be 'agent' or 'manager', got %q", *role)
		}
		body["role"] = r
	}

	// Only build permissions if at least one perm flag was used.
	if passed["permissions"] || passed["all"] || passed["rw"] || passed["read"] || passed["none"] {
		permissions, err := buildTeamPermissions(*permsJSON, *allLevel, *rwKeys, *readKeys, *noneKeys)
		if err != nil {
			return err
		}
		if permissions == nil {
			// Defensive — we got here because a perm flag was set, so we should
			// always end up with a map (possibly empty). Substitute an empty
			// map so the backend's `req.Permissions != nil` branch fires and
			// actually writes the empty payload (i.e. "wipe permissions").
			permissions = map[string]string{}
		}
		body["permissions"] = permissions
	}

	return runReq("PUT", "/app/team/"+memberID, body)
}

// cmdTeamSetPermissions is `team update` with permissions only — a tighter
// alias that refuses any --role flag so it cannot accidentally change a role
// while only intending to change perms.
func cmdTeamSetPermissions(memberID string, args []string) error {
	fs := flag.NewFlagSet("team set-permissions", flag.ContinueOnError)
	permsJSON := fs.String("permissions", "", `permission map JSON, e.g. '{"messages":"read_write"}'`)
	allLevel := fs.String("all", "", "set every known page to this level: none | read | read_write")
	rwKeys := fs.String("rw", "", "comma-separated page keys to grant read_write")
	readKeys := fs.String("read", "", "comma-separated page keys to grant read")
	noneKeys := fs.String("none", "", "comma-separated page keys to grant none")
	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify team set-permissions <member_id> [--all level | --permissions '{…}' | --rw keys | --read keys | --none keys]")
	}
	permissions, err := buildTeamPermissions(*permsJSON, *allLevel, *rwKeys, *readKeys, *noneKeys)
	if err != nil {
		return err
	}
	if permissions == nil {
		permissions = map[string]string{}
	}
	return runReq("PUT", "/app/team/"+memberID, map[string]any{
		"permissions": permissions,
	})
}

// ─── permission-map builder ──────────────────────────────────────────────────

// buildTeamPermissions assembles a final permission map from up to four
// inputs. The merge order is fixed and documented so callers can predict the
// result:
//
//  1. --all <level>     seeds every known page key with that level
//  2. --permissions     overlays a user-supplied JSON map (any keys allowed)
//  3. --rw / --read / --none overlay individual page keys with a chosen level
//
// Returns nil if no input was given, so the caller can distinguish "user did
// not touch permissions" from "user supplied an empty map".
func buildTeamPermissions(permsJSON, allLevel, rwKeys, readKeys, noneKeys string) (map[string]string, error) {
	if permsJSON == "" && allLevel == "" && rwKeys == "" && readKeys == "" && noneKeys == "" {
		return nil, nil
	}

	out := map[string]string{}

	// 1. --all <level>
	if allLevel != "" {
		lvl := strings.ToLower(strings.TrimSpace(allLevel))
		if !teamPermissionLevels[lvl] {
			return nil, fmt.Errorf("--all level must be one of none, read, read_write — got %q", allLevel)
		}
		for _, k := range teamPageKeys {
			out[k] = lvl
		}
	}

	// 2. --permissions <json>
	if permsJSON != "" {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(permsJSON), &parsed); err != nil {
			return nil, fmt.Errorf("--permissions must be a JSON object mapping page keys to levels: %w", err)
		}
		for k, v := range parsed {
			lvl := strings.ToLower(strings.TrimSpace(v))
			if !teamPermissionLevels[lvl] {
				return nil, fmt.Errorf("--permissions[%s] must be none, read, or read_write — got %q", k, v)
			}
			out[k] = lvl
		}
	}

	// 3. --rw / --read / --none — overlay specific keys.
	for _, pair := range []struct {
		keys, level string
	}{
		{rwKeys, "read_write"},
		{readKeys, "read"},
		{noneKeys, "none"},
	} {
		if pair.keys == "" {
			continue
		}
		for _, k := range splitTags(pair.keys) {
			out[k] = pair.level
		}
	}

	return out, nil
}
