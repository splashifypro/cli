package main

import (
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify ctwa` (alias: meta-ads) — the CLI mirror
// of /settings/ctwa. The page wraps three sub-surfaces:
//
//	OAuth code exchange — finishing the Meta connect flow on the backend
//	CAPI config         — Conversion API dataset + event triggers
//	Meta-ads listing    — the per-account ads + per-ad detail
//
// Backed by /api/v1/app/meta-ads/*.

// capiTriggerEvents is the validated set of event names CAPI can fire on.
// Sent verbatim in --lead-event-trigger / --purchase-event-trigger.

// ─── splashify ctwa ──────────────────────────────────────────────────────────

func cmdCTWA(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "status", "info":
		// Default view: the CAPI config (matches the connection-status
		// card the page shows at the top).
		return runReq("GET", "/app/meta-ads/capi", nil)

	case "capi":
		return cmdCTWACAPI(args[1:])

	case "exchange-code", "connect":
		return cmdCTWAExchangeCode(args[1:])

	case "refresh-token":
		// /refresh-token/:user_id takes the calling user's user_id — we
		// resolve it from /app/me so the user doesn't have to.
		return cmdCTWARefreshToken()

	case "ads":
		return cmdCTWAAds()

	case "ad":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify ctwa ad <ad_id>")
		}
		return cmdCTWAGetAd(args[1])

	default:
		return fmt.Errorf("unknown ctwa subcommand: %s\nrun: splashify ctwa", sub)
	}
}

// ─── ctwa capi ──────────────────────────────────────────────────────────────

// cmdCTWACAPI dispatches the CAPI subtree: read, save, send-event.
func cmdCTWACAPI(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "status", "info", "show":
		return runReq("GET", "/app/meta-ads/capi", nil)

	case "save", "set", "update", "edit":
		return cmdCTWACAPISave(args[1:])

	case "send-event", "fire":
		return cmdCTWACAPISendEvent(args[1:])

	default:
		return fmt.Errorf("unknown capi subcommand: %s\nrun: splashify ctwa capi", sub)
	}
}

// cmdCTWACAPISave updates the CAPI config. Backend POSTs to the same path
// as the GET; only the body fields the user explicitly sets are included.
// If --dataset-id is omitted on a fresh save, the backend auto-creates one
// on Meta's Graph API and stamps the new id on the row.
func cmdCTWACAPISave(args []string) error {
	fs := flag.NewFlagSet("ctwa capi save", flag.ContinueOnError)
	datasetID := fs.String("dataset-id", "", "Meta dataset id — leave empty to let the backend create one")

	leadEnabled := fs.String("lead-enabled", "", "true | false")
	leadTrigger := fs.String("lead-trigger", "", "event name that fires Lead conversion")
	leadTag := fs.String("lead-tag", "", "contact tag to apply on lead-event match")

	purchaseEnabled := fs.String("purchase-enabled", "", "true | false")
	purchaseTrigger := fs.String("purchase-trigger", "", "event name that fires Purchase conversion")
	purchaseTag := fs.String("purchase-tag", "", "contact tag to apply on purchase-event match")
	purchaseCurrency := fs.String("purchase-currency", "", `ISO 4217 currency code, e.g. "INR"`)
	purchaseValue := fs.Float64("purchase-value", 0, "default value to send on purchase event")

	if err := fs.Parse(args); err != nil {
		return err
	}
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })
	if len(passed) == 0 {
		return fmt.Errorf("usage: splashify ctwa capi save [--dataset-id …] [--lead-enabled true|false] [--lead-trigger …] [--lead-tag …] [--purchase-enabled true|false] [--purchase-trigger …] [--purchase-tag …] [--purchase-currency INR] [--purchase-value 99.99]")
	}

	body := map[string]any{}
	if passed["dataset-id"] {
		body["dataset_id"] = *datasetID
	}
	if passed["lead-enabled"] {
		b, err := parseBool(*leadEnabled)
		if err != nil {
			return fmt.Errorf("--lead-enabled must be true or false")
		}
		body["lead_event_enabled"] = b
	}
	if passed["lead-trigger"] {
		body["lead_event_trigger"] = *leadTrigger
	}
	if passed["lead-tag"] {
		body["lead_event_tag"] = *leadTag
	}
	if passed["purchase-enabled"] {
		b, err := parseBool(*purchaseEnabled)
		if err != nil {
			return fmt.Errorf("--purchase-enabled must be true or false")
		}
		body["purchase_event_enabled"] = b
	}
	if passed["purchase-trigger"] {
		body["purchase_event_trigger"] = *purchaseTrigger
	}
	if passed["purchase-tag"] {
		body["purchase_event_tag"] = *purchaseTag
	}
	if passed["purchase-currency"] {
		body["purchase_event_currency"] = *purchaseCurrency
	}
	if passed["purchase-value"] {
		body["purchase_event_value"] = *purchaseValue
	}

	return runReq("POST", "/app/meta-ads/capi", body)
}

// cmdCTWACAPISendEvent manually fires a conversion event. Useful for
// testing your dataset_id + trigger config without waiting on a real
// signal.
func cmdCTWACAPISendEvent(args []string) error {
	fs := flag.NewFlagSet("ctwa capi send-event", flag.ContinueOnError)
	eventType := fs.String("event-type", "", `"lead" | "purchase" (required)`)
	phone := fs.String("phone", "", "E.164 phone of the target contact (required)")
	value := fs.Float64("value", 0, "monetary value (purchase events)")
	currency := fs.String("currency", "", "ISO 4217 currency code, e.g. INR")
	if err := fs.Parse(args); err != nil {
		return err
	}
	et := strings.ToLower(strings.TrimSpace(*eventType))
	if et != "lead" && et != "purchase" {
		return fmt.Errorf("--event-type must be lead or purchase — got %q", *eventType)
	}
	if strings.TrimSpace(*phone) == "" {
		return fmt.Errorf("--phone is required")
	}
	body := map[string]any{
		"event_type": et,
		"phone":      *phone,
	}
	if *value != 0 {
		body["value"] = *value
	}
	if *currency != "" {
		body["currency"] = *currency
	}
	return runReq("POST", "/app/meta-ads/capi/send-event", body)
}

// ─── ctwa exchange-code ─────────────────────────────────────────────────────

// cmdCTWAExchangeCode runs the server-side leg of the Meta OAuth code
// exchange. The user runs the OAuth dance in a browser (the in-app
// "Connect Meta" button), copies the `code` from the redirect URL, and
// passes it here.
func cmdCTWAExchangeCode(args []string) error {
	fs := flag.NewFlagSet("ctwa exchange-code", flag.ContinueOnError)
	code := fs.String("code", "", "authorization code from the Meta redirect (required)")
	scopes := fs.String("granted-scopes", "", `granted scopes JSON array, e.g. '["ads_management","pages_show_list"]'`)
	redirect := fs.String("redirect-uri", "", "the redirect URI registered on the Meta app")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*code) == "" {
		return fmt.Errorf("usage: splashify ctwa exchange-code --code <code> [--granted-scopes '[…]'] [--redirect-uri …]")
	}
	body := map[string]any{
		"code": *code,
	}
	if *scopes != "" {
		body["granted_scopes"] = *scopes
	}
	if *redirect != "" {
		body["redirect_uri"] = *redirect
	}
	return runReq("POST", "/app/meta-ads/exchange-code", body)
}

// cmdCTWARefreshToken refreshes the stored Meta token. The endpoint path
// carries the user_id, but it's always the calling user's — we resolve
// it via /app/me so the user doesn't have to look it up.
func cmdCTWARefreshToken() error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	uid, err := resolveSelfUserID(api)
	if err != nil {
		return err
	}
	return runReq("POST", "/app/meta-ads/refresh-token/"+uid, nil)
}

// ─── ctwa ads ───────────────────────────────────────────────────────────────

func cmdCTWAAds() error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	uid, err := resolveSelfUserID(api)
	if err != nil {
		return err
	}
	return runReq("GET", "/app/meta-ads/"+uid, nil)
}

func cmdCTWAGetAd(adID string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)
	uid, err := resolveSelfUserID(api)
	if err != nil {
		return err
	}
	return runReq("GET", "/app/meta-ads/"+uid+"/ad/"+adID, nil)
}
