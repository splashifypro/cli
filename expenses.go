package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// ─── expenses — full mirror of /settings/track-expenses ─────────────────────
//
// The page itself is read-only — three GET endpoints plus a client-side
// CSV export button. The CLI exposes the same surface:
//
//	GET /api/v1/app/expenses/summary?period=…
//	GET /api/v1/app/expenses/trends?period=…
//	GET /api/v1/app/expenses/billing-logs?period=…&limit=…
//
// Surface:
//
//	splashify expenses                              summary (period=30d)
//	splashify expenses summary    [--period 7d|30d|3m|6m|all]
//	splashify expenses categories [--period …]      summary, category-only
//	splashify expenses countries  [--period …]      summary, top_countries only
//	splashify expenses trends     [--period …]      daily deduction series
//	splashify expenses logs       [--period …] [--limit N (max 500)]
//	                              [--category marketing|utility|authentication|rcs|call|broadcast_deduction|broadcast_refund]
//	                              [--country IN] [--free-trial true|false]
//	splashify expenses export     [--period …] [--limit N] [--out file.csv]
//	                                                same shape as the page's
//	                                                "Export CSV" button (no
//	                                                base/markup — final total
//	                                                only)
//
// Aliases: `expenses`, `expense`, `track-expenses`. No subscription / balance
// preflight — every account can read its own deduction history.

// ── periods ─────────────────────────────────────────────────────────────────
// Same set the page exposes — backend rejects anything else by falling back
// to 30d, so we validate up front for a cleaner error.
var expensePeriods = map[string]bool{
	"7d": true, "30d": true, "3m": true, "6m": true, "all": true,
}

func validateExpensePeriod(p string) error {
	if p == "" {
		return nil
	}
	if !expensePeriods[p] {
		return fmt.Errorf("--period must be one of: 7d, 30d, 3m, 6m, all")
	}
	return nil
}

func cmdExpenses(args []string) error {
	if len(args) == 0 {
		return runReq("GET", "/app/expenses/summary?period=30d", nil)
	}
	switch args[0] {
	case "summary":
		return cmdExpensesSummary(args[1:], "")
	case "categories", "category", "breakdown":
		return cmdExpensesSummary(args[1:], "categories")
	case "countries", "top-countries":
		return cmdExpensesSummary(args[1:], "countries")
	case "trends", "trend", "chart":
		return cmdExpensesTrends(args[1:])
	case "logs", "log", "transactions":
		return cmdExpensesLogs(args[1:])
	case "export", "csv":
		return cmdExpensesExport(args[1:])
	default:
		return fmt.Errorf(`unknown expenses action: %s
usage: splashify expenses [summary|categories|countries|trends|logs|export]`, args[0])
	}
}

// cmdExpensesSummary backs `summary`, `categories`, `countries`. The `view`
// trims the JSON to just the requested slice so users don't have to pipe to jq
// for the most common breakdowns.
func cmdExpensesSummary(args []string, view string) error {
	fs := flag.NewFlagSet("expenses summary", flag.ContinueOnError)
	period := fs.String("period", "30d", "7d | 30d | 3m | 6m | all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateExpensePeriod(*period); err != nil {
		return err
	}
	path := withQuery("/app/expenses/summary", map[string]string{"period": *period})

	if view == "" {
		return runReq("GET", path, nil)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", path, nil)
	if err != nil {
		return err
	}
	var resp struct {
		Success bool           `json:"success"`
		Summary map[string]any `json:"summary"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Summary == nil {
		printJSON(raw)
		return nil
	}

	var out map[string]any
	switch view {
	case "categories":
		out = map[string]any{
			"success":          true,
			"period":           resp.Summary["period"],
			"currency":         resp.Summary["currency"],
			"total_deducted":   resp.Summary["total_deducted"],
			"total_messages":   resp.Summary["total_messages"],
			"marketing_amount": resp.Summary["marketing_amount"],
			"marketing_count":  resp.Summary["marketing_count"],
			"utility_amount":   resp.Summary["utility_amount"],
			"utility_count":    resp.Summary["utility_count"],
			"auth_amount":      resp.Summary["auth_amount"],
			"auth_count":       resp.Summary["auth_count"],
			"rcs_amount":       resp.Summary["rcs_amount"],
			"rcs_count":        resp.Summary["rcs_count"],
			"call_amount":      resp.Summary["call_amount"],
			"call_count":       resp.Summary["call_count"],
		}
	case "countries":
		out = map[string]any{
			"success":        true,
			"period":         resp.Summary["period"],
			"currency":       resp.Summary["currency"],
			"total_deducted": resp.Summary["total_deducted"],
			"top_countries":  resp.Summary["top_countries"],
		}
	}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func cmdExpensesTrends(args []string) error {
	fs := flag.NewFlagSet("expenses trends", flag.ContinueOnError)
	period := fs.String("period", "30d", "7d | 30d | 3m | 6m | all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateExpensePeriod(*period); err != nil {
		return err
	}
	return runReq("GET", withQuery("/app/expenses/trends",
		map[string]string{"period": *period}), nil)
}

func cmdExpensesLogs(args []string) error {
	fs := flag.NewFlagSet("expenses logs", flag.ContinueOnError)
	period := fs.String("period", "30d", "7d | 30d | 3m | 6m | all")
	limit := fs.String("limit", "100", "1–500 (backend max 500)")
	category := fs.String("category", "", "client-side filter: marketing | utility | authentication | rcs | call | broadcast_deduction | broadcast_refund")
	country := fs.String("country", "", "client-side filter: ISO country code, e.g. IN")
	freeTrial := fs.String("free-trial", "", "client-side filter: true | false")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateExpensePeriod(*period); err != nil {
		return err
	}
	path := withQuery("/app/expenses/billing-logs", map[string]string{
		"period": *period,
		"limit":  *limit,
	})

	// No client-side filters → server response, untouched.
	if *category == "" && *country == "" && *freeTrial == "" {
		return runReq("GET", path, nil)
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", path, nil)
	if err != nil {
		return err
	}
	var resp struct {
		Success bool             `json:"success"`
		Logs    []map[string]any `json:"logs"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		printJSON(raw)
		return nil
	}

	wantCat := strings.ToLower(*category)
	wantCountry := strings.ToUpper(*country)
	matched := make([]map[string]any, 0, len(resp.Logs))
	for _, l := range resp.Logs {
		if wantCat != "" {
			c, _ := l["category"].(string)
			if !strings.EqualFold(c, wantCat) {
				continue
			}
		}
		if wantCountry != "" {
			c, _ := l["country_iso"].(string)
			if !strings.EqualFold(c, wantCountry) {
				continue
			}
		}
		if *freeTrial != "" {
			ft, _ := l["is_free_trial"].(bool)
			want := strings.EqualFold(*freeTrial, "true")
			if ft != want {
				continue
			}
		}
		matched = append(matched, l)
	}
	out := map[string]any{"success": true, "logs": matched, "total": len(matched)}
	encoded, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

// cmdExpensesExport mirrors the page's "Export CSV" button. Same columns,
// same UTF-8 BOM so Excel renders ₹ correctly. Default destination is stdout;
// pass --out file.csv to write to disk.
func cmdExpensesExport(args []string) error {
	fs := flag.NewFlagSet("expenses export", flag.ContinueOnError)
	period := fs.String("period", "30d", "7d | 30d | 3m | 6m | all")
	limit := fs.String("limit", "500", "1–500 (backend max 500)")
	out := fs.String("out", "", "write CSV here (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateExpensePeriod(*period); err != nil {
		return err
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	path := withQuery("/app/expenses/billing-logs", map[string]string{
		"period": *period,
		"limit":  *limit,
	})
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).callRaw("GET", path, nil)
	if err != nil {
		return err
	}
	var resp struct {
		Success bool `json:"success"`
		Logs    []struct {
			ID               string  `json:"id"`
			RecipientPhone   string  `json:"recipient_phone"`
			Category         string  `json:"category"`
			CountryISO       string  `json:"country_iso"`
			TotalAmount      float64 `json:"total_amount"`
			IsFreeTrial      bool    `json:"is_free_trial"`
			DeductionTrigger string  `json:"deduction_trigger"`
			Description      string  `json:"description"`
			CreatedAt        string  `json:"created_at"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode billing logs: %w", err)
	}

	var dst *os.File
	if *out == "" {
		dst = os.Stdout
	} else {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("create %s: %w", *out, err)
		}
		defer f.Close()
		dst = f
	}

	// UTF-8 BOM so Excel renders ₹ instead of the â,¹ mojibake. Escaped
	// () so the bytes aren't interpreted by Go's parser as a file BOM.
	if _, err := dst.WriteString("\uFEFF"); err != nil {
		return err
	}
	w := csv.NewWriter(dst)
	if err := w.Write([]string{
		"Date", "Recipient", "Category", "Country",
		"Total Deducted", "Trigger", "Free Trial",
	}); err != nil {
		return err
	}
	for _, l := range resp.Logs {
		recipient := l.RecipientPhone
		if recipient == "" {
			recipient = l.Description
		}
		if recipient == "" {
			recipient = "—"
		}
		date := l.CreatedAt
		if t, err := time.Parse(time.RFC3339, l.CreatedAt); err == nil {
			date = t.Format("02 Jan 2006 03:04 PM")
		}
		ft := "No"
		if l.IsFreeTrial {
			ft = "Yes"
		}
		if err := w.Write([]string{
			date,
			recipient,
			l.Category,
			l.CountryISO,
			fmt.Sprintf("₹%.4f", l.TotalAmount),
			l.DeductionTrigger,
			ft,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if *out != "" {
		fmt.Fprintf(os.Stderr, "wrote %d rows to %s\n", len(resp.Logs), *out)
	}
	return nil
}
