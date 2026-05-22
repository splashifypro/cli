package main

import (
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify allowed-ips` (aliases: ip-allowlist, ips)
// — the CLI mirror of /settings/allowed-ips. The endpoint is **paid** —
// trial users get HTTP 402 with code `feature_locked`; the backend's
// standard tier-gate error is surfaced via the CLI's existing
// plan_required formatter in api.go.
//
// Backed by /api/v1/app/ip-allowlist/*. Notably this endpoint family
// **skips** the ipAllowlistGate middleware so users can always reach it
// to fix their own configuration — no risk of locking yourself out.

// ipModes is the validated mode set the backend accepts on POST.
var ipModes = map[string]bool{"single": true, "range": true}

// ─── splashify allowed-ips ───────────────────────────────────────────────────

func cmdAllowedIPs(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "list", "ls":
		return runReq("GET", "/app/ip-allowlist", nil)

	case "add", "create":
		return cmdAllowedIPsAdd(args[1:])

	case "delete", "remove", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify allowed-ips delete <entry_id>")
		}
		return runReq("DELETE", "/app/ip-allowlist/"+args[1], nil)

	default:
		return fmt.Errorf("unknown allowed-ips subcommand: %s\nrun: splashify allowed-ips", sub)
	}
}

// cmdAllowedIPsAdd POSTs a single new entry. Validation mirrors the backend
// for a friendlier error before the round-trip — names are capped at 80
// chars, single-mode entries take --ip, range-mode entries take --start +
// --end (both IPv4).
func cmdAllowedIPsAdd(args []string) error {
	fs := flag.NewFlagSet("allowed-ips add", flag.ContinueOnError)
	name := fs.String("name", "", `friendly name (required, max 80 chars), e.g. "Bangalore office"`)
	mode := fs.String("mode", "single", "single | range")
	ip := fs.String("ip", "", "IPv4 address — required when --mode=single")
	start := fs.String("start", "", "range start IPv4 — required when --mode=range")
	end := fs.String("end", "", "range end IPv4 — required when --mode=range")
	if err := fs.Parse(args); err != nil {
		return err
	}

	*name = strings.TrimSpace(*name)
	if *name == "" {
		return fmt.Errorf("--name is required\nusage: splashify allowed-ips add --name \"Office\" --mode single --ip 203.0.113.4\n   or: splashify allowed-ips add --name \"VPN range\" --mode range --start 10.0.0.0 --end 10.0.0.255")
	}
	if len(*name) > 80 {
		return fmt.Errorf("--name must be 80 characters or less (got %d)", len(*name))
	}
	m := strings.ToLower(strings.TrimSpace(*mode))
	if !ipModes[m] {
		return fmt.Errorf("--mode must be single or range — got %q", *mode)
	}

	body := map[string]any{
		"name": *name,
		"mode": m,
	}
	if m == "single" {
		if strings.TrimSpace(*ip) == "" {
			return fmt.Errorf("--ip is required when --mode=single")
		}
		body["ip_value"] = *ip
	} else {
		if strings.TrimSpace(*start) == "" || strings.TrimSpace(*end) == "" {
			return fmt.Errorf("--start and --end are both required when --mode=range")
		}
		body["range_start"] = *start
		body["range_end"] = *end
	}

	return runReq("POST", "/app/ip-allowlist", body)
}
