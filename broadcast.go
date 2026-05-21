package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ─── broadcasts — full surface of /broadcasts and /broadcasts/<id> ───────────
//
// One command (`splashify broadcasts` for the list, `splashify broadcast`
// for everything per-id and create/cancel/restart/send-now/rebroadcast).
//
//   READ:
//     splashify broadcasts [--status …] [--search …] [--page N] [--limit N]
//     splashify broadcast stats [--period 7d|30d|all]
//     splashify broadcast <id> [--recompute]
//     splashify broadcast <id> messages [--status … --channel … --page --limit --search --cumulative]
//     splashify broadcast <id> cohorts        cohort-counts (failed / sent / delivered / read)
//     splashify broadcast <id> export [--status ALL|FAILED|DELIVERED|...] [--csv]
//
//   WRITE (gated by subscription + wallet balance — see preflightBroadcast):
//     splashify broadcast create   …             POST /app/broadcasts (WhatsApp or RCS)
//     splashify broadcast <id> cancel
//     splashify broadcast <id> restart
//     splashify broadcast <id> send-now
//     splashify broadcast <id> rebroadcast --cohort failed|sent|delivered|read|all [flags…]
//
// Before any write command that triggers actual sends, the CLI runs a
// preflight: hit /app/developer/cli-eligibility (subscription + WABA gate
// — same one cmdConnect uses) and /app/wallet/info. A subscription_expired
// account is refused with an upgrade prompt; an empty wallet is refused
// with a recharge prompt. --yes skips the balance soft-check (the backend
// still enforces it hard via "Insufficient balance" 4xx).

func cmdBroadcasts(args []string) error {
	fs := flag.NewFlagSet("broadcasts", flag.ContinueOnError)
	status := fs.String("status", "", "filter by status (scheduled, sending, completed, cancelled, paused, …)")
	search := fs.String("search", "", "search by campaign name")
	page := fs.String("page", "", "page number")
	limit := fs.String("limit", "", "items per page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/broadcasts", map[string]string{
		"status": *status, "search": *search, "page": *page, "limit": *limit,
	}), nil)
}

func cmdBroadcast(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify broadcast <id|stats|create> …")
	}

	switch args[0] {
	case "stats":
		fs := flag.NewFlagSet("broadcast stats", flag.ContinueOnError)
		period := fs.String("period", "", "7d, 30d, 90d, all (default: all)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/app/broadcasts/stats"
		if *period != "" {
			path += "?period=" + url.QueryEscape(*period)
		}
		return runReq("GET", path, nil)

	case "create":
		return cmdBroadcastCreate(args[1:])
	}

	// First arg is treated as a broadcast id; optional sub-action follows.
	id := args[0]
	if len(args) == 1 {
		// `splashify broadcast <id> [--recompute]` lands here via reparse below.
		return cmdBroadcastGet(id, nil)
	}

	switch args[1] {
	case "messages":
		return cmdBroadcastMessages(id, args[2:])
	case "cohorts", "cohort-counts":
		return runReq("GET", "/app/broadcasts/"+id+"/cohort-counts", nil)
	case "export":
		return cmdBroadcastExport(id, args[2:])
	case "cancel":
		if err := preflightSend("broadcasts",false /*requireBalance*/); err != nil {
			return err
		}
		return runReq("POST", "/app/broadcasts/"+id+"/cancel", nil)
	case "restart":
		if err := preflightSend("broadcasts",true); err != nil {
			return err
		}
		return runReq("POST", "/app/broadcasts/"+id+"/restart", nil)
	case "send-now":
		if err := preflightSend("broadcasts",true); err != nil {
			return err
		}
		return runReq("POST", "/app/broadcasts/"+id+"/send-now", nil)
	case "rebroadcast":
		return cmdBroadcastRebroadcast(id, args[2:])
	default:
		// Allow `broadcast <id> --recompute` by reparsing flags.
		return cmdBroadcastGet(id, args[1:])
	}
}

// ─── reads ───────────────────────────────────────────────────────────────────

func cmdBroadcastGet(id string, args []string) error {
	fs := flag.NewFlagSet("broadcast get", flag.ContinueOnError)
	recompute := fs.Bool("recompute", false, "ask backend to recompute stats")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "/app/broadcasts/" + id
	if *recompute {
		path += "?recompute=true"
	}
	return runReq("GET", path, nil)
}

func cmdBroadcastMessages(id string, args []string) error {
	fs := flag.NewFlagSet("broadcast messages", flag.ContinueOnError)
	status := fs.String("status", "", "filter: SENT, DELIVERED, READ, FAILED, …")
	channel := fs.String("channel", "", "filter: whatsapp, rcs")
	page := fs.String("page", "", "page number")
	limit := fs.String("limit", "", "items per page")
	search := fs.String("search", "", "search by phone / contact name")
	cumulative := fs.String("cumulative", "", "true|false — include earlier statuses (DELIVERED also includes READ)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/broadcasts/"+id+"/messages", map[string]string{
		"status":     *status,
		"channel":    *channel,
		"page":       *page,
		"limit":      *limit,
		"search":     *search,
		"cumulative": *cumulative,
	}), nil)
}

func cmdBroadcastExport(id string, args []string) error {
	fs := flag.NewFlagSet("broadcast export", flag.ContinueOnError)
	status := fs.String("status", "ALL", "ALL, SENT, DELIVERED, READ, FAILED, …")
	csv := fs.Bool("csv", false, "request format=csv (binary stream; pipe to a file)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := map[string]string{"status": *status}
	if *csv {
		q["format"] = "csv"
	}
	return runReq("GET", withQuery("/app/broadcasts/"+id+"/export", q), nil)
}

// ─── writes (preflight-gated) ────────────────────────────────────────────────

func cmdBroadcastCreate(args []string) error {
	fs := flag.NewFlagSet("broadcast create", flag.ContinueOnError)
	name := fs.String("name", "", "campaign name (required)")
	desc := fs.String("description", "", "optional description")

	// Template fields (required: name + category + language; ID is optional).
	tmplID := fs.String("template-id", "", "template_id (optional — name+language is enough)")
	tmplName := fs.String("template", "", "approved template name (required)")
	tmplCat := fs.String("category", "", "template_category: MARKETING | UTILITY | AUTHENTICATION (required)")
	tmplLang := fs.String("language", "en", "template_language code (default: en)")
	tmplParams := fs.String("params", "", `JSON for template_params (per Meta Cloud API, e.g. {"components":[…]})`)
	mediaURL := fs.String("media-url", "", "media URL for header media templates")

	// Audience.
	segmentIDs := fs.String("segment-ids", "", "comma-separated segment IDs")
	contactIDs := fs.String("contact-ids", "", "comma-separated contact IDs")

	// Scheduling + throttling.
	sendType := fs.String("send-type", "now", "now | scheduled (default: now)")
	scheduled := fs.String("scheduled-at", "", "ISO 8601 — required when --send-type=scheduled")
	rateLimit := fs.Int("rate-limit", 0, "messages per second (0 = backend default)")
	batchSize := fs.Int("batch-size", 0, "batch size for sending (0 = backend default)")

	// RCS fallback for WhatsApp broadcasts, or pure-RCS mode.
	rcsFbID := fs.String("rcs-fallback-template-id", "", "RCS fallback template id (WA→RCS fallback)")
	rcsFbName := fs.String("rcs-fallback-template-name", "", "RCS fallback template name")
	rcs := fs.Bool("rcs", false, "send as a pure-RCS broadcast (channel=rcs)")

	// Preflight override.
	yes := fs.Bool("yes", false, "skip the balance soft-check and confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *name == "" || *tmplName == "" || *tmplCat == "" {
		return fmt.Errorf(`usage: splashify broadcast create \
  --name "May launch" --template <approved_name> \
  --category MARKETING --language en \
  [--template-id <id>] [--params '{"components":[…]}'] [--media-url …] \
  [--segment-ids id1,id2 | --contact-ids id1,id2] \
  [--send-type now|scheduled] [--scheduled-at 2026-06-01T10:00:00Z] \
  [--rate-limit N] [--batch-size N] \
  [--rcs-fallback-template-id <id> --rcs-fallback-template-name <name> | --rcs] \
  [--yes]`)
	}
	if *segmentIDs == "" && *contactIDs == "" {
		return fmt.Errorf("at least one of --segment-ids or --contact-ids is required")
	}
	if *sendType == "scheduled" && *scheduled == "" {
		return fmt.Errorf("--scheduled-at is required when --send-type=scheduled")
	}

	// Preflight — subscription + non-zero wallet.
	if !*yes {
		if err := preflightSend("broadcasts",true); err != nil {
			return err
		}
	}

	body := map[string]any{
		"name":              *name,
		"template_name":     strings.ToLower(strings.TrimSpace(*tmplName)),
		"template_category": strings.ToUpper(strings.TrimSpace(*tmplCat)),
		"template_language": *tmplLang,
		"send_type":         *sendType,
	}
	if *desc != "" {
		body["description"] = *desc
	}
	if *tmplID != "" {
		body["template_id"] = *tmplID
	}
	if *tmplParams != "" {
		var parsed any
		if err := json.Unmarshal([]byte(*tmplParams), &parsed); err != nil {
			return fmt.Errorf("--params must be valid JSON: %w", err)
		}
		body["template_params"] = parsed
	}
	if *mediaURL != "" {
		body["media_url"] = *mediaURL
	}
	if *segmentIDs != "" {
		body["segment_ids"] = splitTags(*segmentIDs)
	}
	if *contactIDs != "" {
		body["contact_ids"] = splitTags(*contactIDs)
	}
	if *scheduled != "" {
		body["scheduled_at"] = *scheduled
	}
	if *rateLimit > 0 {
		body["rate_limit"] = *rateLimit
	}
	if *batchSize > 0 {
		body["batch_size"] = *batchSize
	}
	if *rcsFbID != "" {
		body["rcs_fallback_template_id"] = *rcsFbID
	}
	if *rcsFbName != "" {
		body["rcs_fallback_template_name"] = *rcsFbName
	}
	if *rcs {
		body["channel"] = "rcs"
		body["channels"] = []string{"rcs"}
	}

	return runReq("POST", "/app/broadcasts", body)
}

func cmdBroadcastRebroadcast(id string, args []string) error {
	fs := flag.NewFlagSet("broadcast rebroadcast", flag.ContinueOnError)
	cohort := fs.String("cohort", "", "failed | sent | delivered | read | all (required)")
	name := fs.String("name", "", "campaign name for the new broadcast")
	tmplID := fs.String("template-id", "", "override template_id")
	tmplName := fs.String("template", "", "override template_name")
	tmplParams := fs.String("params", "", "JSON for template_params")
	mediaURL := fs.String("media-url", "", "override media_url")
	sendAt := fs.String("send-at", "", "ISO 8601 — schedule the rebroadcast (default: now)")
	yes := fs.Bool("yes", false, "skip the balance soft-check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch strings.ToLower(*cohort) {
	case "failed", "sent", "delivered", "read", "all":
	default:
		return fmt.Errorf(`usage: splashify broadcast <id> rebroadcast --cohort failed|sent|delivered|read|all [flags…]`)
	}

	if !*yes {
		if err := preflightSend("broadcasts",true); err != nil {
			return err
		}
	}

	body := map[string]any{"cohort": strings.ToLower(*cohort)}
	if *name != "" {
		body["name"] = *name
	}
	if *tmplID != "" {
		body["template_id"] = *tmplID
	}
	if *tmplName != "" {
		body["template_name"] = *tmplName
	}
	if *tmplParams != "" {
		body["template_params"] = *tmplParams
	}
	if *mediaURL != "" {
		body["media_url"] = *mediaURL
	}
	if *sendAt != "" {
		body["send_at"] = *sendAt
	}

	return runReq("POST", "/app/broadcasts/"+id+"/rebroadcast", body)
}

// ─── preflight ───────────────────────────────────────────────────────────────
//
// preflightSend runs before any command that triggers actual sends/charges
// (broadcast create / send-now / restart / rebroadcast, call initiate, the
// calling send-* template-message commands). Two checks:
//
//  1. /app/developer/cli-eligibility — same gate cmdConnect uses. Refuses
//     with the documented "upgrade your plan" / "no WABA" / "suspended"
//     messages. SPLASHIFY_SKIP_ELIGIBILITY=1 still bypasses.
//
//  2. /app/wallet/info — fetches the user's wallet balance. When
//     requireBalance is true we refuse if wallet_amount <= 0; otherwise
//     we just show the current balance as context.
//
// `action` is the human-readable feature name woven into the refusal
// messages ("broadcasts", "calls", "templated call messages"). The
// backend always re-enforces both (subscription middleware and
// walletBilling.CheckBalance returning "Insufficient balance" 4xx) — this
// preflight is for fast, friendly UX, not security.
func preflightSend(action string, requireBalance bool) error {
	if v := os.Getenv("SPLASHIFY_SKIP_PREFLIGHT"); v != "" && v != "0" && v != "false" {
		fmt.Fprintln(os.Stderr, "note: SPLASHIFY_SKIP_PREFLIGHT is set — skipping preflight")
		return nil
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	// 1. Subscription / eligibility
	if v := os.Getenv("SPLASHIFY_SKIP_ELIGIBILITY"); v == "" || v == "0" || v == "false" {
		elig, err := api.cliEligibility()
		if err == nil && !elig.Eligible {
			switch elig.Reason {
			case "subscription_expired":
				url := elig.UpgradeURL
				if url == "" {
					url = "https://app.splashifypro.com/settings/subscriptions"
				}
				return fmt.Errorf(`%s are unavailable — your trial has ended and there is no active paid plan on this account.

  Upgrade your plan:  %s

  Once a plan is active, retry the command.`, action, url)
			case "no_waba":
				return fmt.Errorf("%s are unavailable — connect a WhatsApp Business Account first (https://app.splashifypro.com → WhatsApp → Connect Number)", action)
			case "account_suspended":
				return fmt.Errorf("%s are unavailable — your account is not active. Contact support", action)
			}
		}
	}

	// 2. Wallet balance
	walletRaw, err := api.callRaw("GET", "/app/wallet/info", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "note: could not fetch wallet balance —", err)
		return nil
	}
	var wallet struct {
		WalletAmount float64 `json:"wallet_amount"`
		SpentBalance float64 `json:"spent_balance"`
		CurrencyCode string  `json:"currency_code"`
	}
	if err := json.Unmarshal(walletRaw, &wallet); err != nil {
		fmt.Fprintln(os.Stderr, "note: wallet response unparsable, continuing")
		return nil
	}
	sym := "₹"
	if strings.EqualFold(wallet.CurrencyCode, "USD") {
		sym = "$"
	}
	fmt.Fprintf(os.Stderr, "Wallet balance: %s%.2f\n", sym, wallet.WalletAmount)
	if requireBalance && wallet.WalletAmount <= 0 {
		return fmt.Errorf(`%s are unavailable — your wallet balance is %s%.2f.

  Recharge: https://app.splashifypro.com/dashboard
  (Or run with --yes to skip this soft-check; the backend still enforces it per-message.)`, action, sym, wallet.WalletAmount)
	}
	return nil
}
