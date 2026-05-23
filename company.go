package main

import (
	"encoding/json"
	"flag"
	"fmt"
)

// This file implements `splashify company` — the CLI mirror of the app's
// /settings/company-details page. It manages the calling org's company
// profile (used on invoices, GST/tax docs, and Meta-facing forms).
//
//	splashify company                 show current company details
//	splashify company update --company-name "Acme Pvt Ltd" --industry "E-commerce" \
//	                        --company-size 51-200 --website https://acme.com \
//	                        --country IN --address1 "…" --address2 "…" \
//	                        --state KA --pincode 560001 --timezone Asia/Calcutta
//
// Backed by /api/v1/app/company-details. Update is a full PUT — the
// backend rejects partial bodies. To keep usage friendly the CLI reads
// the current state and merges your flag values before sending, so you
// only need to pass what's changing.

func cmdCompany(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "show", "get", "details":
		return runReq("GET", "/app/company-details", nil)

	case "update", "edit", "set":
		return cmdCompanyUpdate(args[1:])

	default:
		return fmt.Errorf("unknown company subcommand: %s\n"+
			"run: splashify company  (or update)", sub)
	}
}

// cmdCompanyUpdate read-modify-writes /app/company-details so callers only
// need to pass the fields they're changing. The backend requires the full
// shape on PUT; the merge happens here.
func cmdCompanyUpdate(args []string) error {
	fs := flag.NewFlagSet("company update", flag.ContinueOnError)
	companyName := fs.String("company-name", "", "registered company name")
	industry := fs.String("industry", "", "industry (e.g. E-commerce, SaaS / Technology)")
	size := fs.String("company-size", "", "1-10 | 11-50 | 51-200 | 201-500 | 501-1000 | 1000+")
	website := fs.String("website", "", "company website URL")
	country := fs.String("country", "", "ISO-2 country code (e.g. IN, US)")
	address1 := fs.String("address1", "", "address line 1")
	address2 := fs.String("address2", "", "address line 2")
	state := fs.String("state", "", "state / province")
	pincode := fs.String("pincode", "", "postal / zip code")
	timezone := fs.String("timezone", "", "IANA timezone (e.g. Asia/Calcutta)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	api := newAPIClient(cfg.BaseURL, cfg.Token)

	// Read current state so we can merge — the backend rejects partial PUTs.
	cur, err := api.callRaw("GET", "/app/company-details", nil)
	if err != nil {
		return fmt.Errorf("load current company details: %w", err)
	}
	var wrap struct {
		Success bool                   `json:"success"`
		Company map[string]any         `json:"company"`
	}
	body := map[string]any{}
	if jerr := json.Unmarshal(cur, &wrap); jerr == nil && wrap.Company != nil {
		for k, v := range wrap.Company {
			body[k] = v
		}
	}

	overrides := map[string]string{
		"company_name": *companyName,
		"industry":     *industry,
		"company_size": *size,
		"website":      *website,
		"country":      *country,
		"address1":     *address1,
		"address2":     *address2,
		"state":        *state,
		"pincode":      *pincode,
		"timezone":     *timezone,
	}
	any := false
	for k, v := range overrides {
		if v != "" {
			body[k] = v
			any = true
		}
	}
	if !any {
		return fmt.Errorf("nothing to update — pass at least one --field value")
	}

	// Ensure every required string key exists (backend rejects missing).
	for _, k := range []string{"company_name", "industry", "company_size", "website",
		"country", "address1", "address2", "state", "pincode", "timezone"} {
		if _, ok := body[k]; !ok {
			body[k] = ""
		}
	}
	return runReq("PUT", "/app/company-details", body)
}
