package main

import (
	"encoding/json"
	"fmt"
)

// ─── credits — read-only view of AI + Voice AI credit state ──────────────────
//
// Mirrors the credit widgets on /dashboard. Read-only — every recharge,
// payment, and verification flow stays in the web app. The CLI just shows
// what's currently on the account.
//
// Three commands cover this surface — pick by the dashboard card you want:
//
//	splashify credits                       consolidated overview (all three at once)
//	splashify ai-credits                    AI credit balance card (/app/ai-credits/info)
//	splashify voice-credits                 Voice AI rate + balance card (/app/voice-ai/rate)
//
// `splashify credits` keeps the subcommands too for parity:
//
//	splashify credits ai                    same as `splashify ai-credits`
//	splashify credits transactions          /app/ai-credits/transactions
//	splashify credits voice                 same as `splashify voice-credits`
//	splashify credits agents                /app/voice-ai/agents (voice AI agent list)
//
// Voice-AI rate response carries everything the dashboard widget shows:
//
//	{
//	  "rate_per_minute":          5.00,
//	  "trial_credited":           true,
//	  "trial_minutes_remaining":  10,
//	  "ai_credit_balance":        1234.56,
//	  "available_minutes":        246
//	}
//
// `available_minutes` is the spendable runway = `ai_credit_balance` ÷
// `rate_per_minute` + any remaining trial minutes. Use it to gauge when
// the next recharge will be needed.

func cmdCredits(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "ai", "info":
		return runReq("GET", "/app/ai-credits/info", nil)
	case "transactions", "tx":
		return runReq("GET", "/app/ai-credits/transactions", nil)
	case "voice", "voice-ai":
		return runReq("GET", "/app/voice-ai/rate", nil)
	case "agents":
		return runReq("GET", "/app/voice-ai/agents", nil)
	case "", "all", "overview":
		return cmdCreditsOverview()
	default:
		return fmt.Errorf("unknown credits subcommand: %s\nrun: splashify credits", sub)
	}
}

// cmdAICredits is the top-level `splashify ai-credits` command — a focused
// view of the AI credit balance card on /dashboard. Goes straight to
// /app/ai-credits/info (returning ai_credit_amount, last_recharge,
// tax_name, tax_percentage). The `transactions` subcommand hits the
// transaction-history endpoint used by /ai-credits → "Transaction history".
func cmdAICredits(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "info", "balance":
		return runReq("GET", "/app/ai-credits/info", nil)
	case "transactions", "tx", "history":
		return runReq("GET", "/app/ai-credits/transactions", nil)
	default:
		return fmt.Errorf("unknown ai-credits subcommand: %s\nrun: splashify ai-credits [transactions]", sub)
	}
}

// cmdVoiceCredits is the top-level `splashify voice-credits` command —
// a focused view of the Voice AI widget on /dashboard. Defaults to
// /app/voice-ai/rate which returns the per-minute rate, the underlying
// ai_credit_balance, available_minutes, trial state, and the
// platform/reseller breakdown. The `agents` subcommand lists the user's
// configured voice AI agents.
func cmdVoiceCredits(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "rate", "info", "balance":
		return runReq("GET", "/app/voice-ai/rate", nil)
	case "agents":
		return runReq("GET", "/app/voice-ai/agents", nil)
	default:
		return fmt.Errorf("unknown voice-credits subcommand: %s\nrun: splashify voice-credits [agents]", sub)
	}
}

// cmdCreditsOverview fetches every credit-related endpoint the dashboard
// widget touches and prints them as one merged JSON object. Sections that
// fail are reported per-section as `{"error":"..."}` so the rest of the
// view still renders — matches the dashboard's degraded-mode behaviour.
func cmdCreditsOverview() error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	fetch := func(path string) json.RawMessage {
		out, err := api.callRaw("GET", path, nil)
		if err != nil {
			return json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error()))
		}
		return out
	}

	combined := map[string]json.RawMessage{
		"ai_credit_info":  fetch("/app/ai-credits/info"),
		"voice_ai_rate":   fetch("/app/voice-ai/rate"),
		"voice_ai_agents": fetch("/app/voice-ai/agents"),
	}

	out, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	fmt.Println(string(out))
	return nil
}
