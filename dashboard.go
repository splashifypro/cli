package main

import (
	"encoding/json"
	"fmt"
)

// This file implements `splashify dashboard` — the CLI mirror of the
// app's home /dashboard page. It returns a consolidated snapshot of the
// signals the page surfaces: WABA + phone status, setup checklist, plan,
// wallet balance, AI-credit balance, and KYC verification state.
//
//	splashify dashboard                  consolidated snapshot (default)
//	splashify dashboard setup-status     just the setup checklist
//	splashify dashboard whatsapp-status  just the WhatsApp/Meta status
//	splashify dashboard kyc              KYC verification status
//
// All endpoints are read-only. Writes (sync-meta, register-phone, OBA
// apply, etc.) live under `splashify waba` to keep the dashboard surface
// focused on observation.

func cmdDashboard(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "overview", "details":
		return cmdDashboardOverview()

	case "setup-status", "setup":
		return runReq("GET", "/app/dashboard/setup-status", nil)

	case "whatsapp-status", "whatsapp", "wa":
		return runReq("GET", "/app/dashboard/whatsapp-status", nil)

	case "kyc", "kyc-status":
		return runReq("GET", "/app/kyc/status", nil)

	case "embedded-signup-config", "es-config":
		return runReq("GET", "/app/dashboard/embedded-signup/config", nil)

	default:
		return fmt.Errorf("unknown dashboard subcommand: %s\n"+
			"run: splashify dashboard  (or setup-status | whatsapp-status | kyc)", sub)
	}
}

// cmdDashboardOverview merges the six endpoints the /dashboard page hits
// on first load into a single JSON blob. Missing or failing endpoints
// degrade gracefully — the caller still sees whichever sections did
// resolve. Mirrors the consolidation pattern in cmdAccount /
// cmdBilling / cmdSubscription.
func cmdDashboardOverview() error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	fetch := func(path string) json.RawMessage {
		raw, err := api.callRaw("GET", path, nil)
		if err != nil {
			return json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
		}
		return raw
	}

	out := map[string]json.RawMessage{
		"setup_status":     fetch("/app/dashboard/setup-status"),
		"whatsapp_status":  fetch("/app/dashboard/whatsapp-status"),
		"plan":             fetch("/app/plans/subscription"),
		"wallet":           fetch("/app/wallet/info"),
		"ai_credits":       fetch("/app/ai-credits/info"),
		"kyc":              fetch("/app/kyc/status"),
	}
	raw, jerr := json.Marshal(out)
	if jerr != nil {
		return fmt.Errorf("marshal overview: %w", jerr)
	}
	printJSON(raw)
	return nil
}
