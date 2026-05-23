package main

import (
	"flag"
	"fmt"
	"strings"
)

// This file implements `splashify profile` — the CLI mirror of the app's
// /profile page. It covers everything the page exposes:
//
//	splashify profile                              show current profile
//	splashify profile update --first-name … --last-name …
//	splashify profile change-password --current … --new …
//	splashify profile whatsapp send-otp --country-code +91 --mobile 9876543210
//	splashify profile whatsapp verify   --country-code +91 --mobile 9876543210 --otp 123456
//	splashify profile picture <path>               upload a new profile picture
//	splashify profile 2fa enable | disable         toggle two-factor auth
//
// Backed by /api/v1/app/profile/*. Reads use /app/me (same as `splashify
// account info`) so a bare `splashify profile` works without any extra
// state.

func cmdProfile(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "show", "info", "me":
		return runReq("GET", "/app/me", nil)

	case "update", "edit":
		return cmdProfileUpdate(args[1:])

	case "change-password", "password", "passwd":
		return cmdProfileChangePassword(args[1:])

	case "whatsapp", "wa":
		return cmdProfileWhatsApp(args[1:])

	case "picture", "avatar", "photo":
		if len(args) < 2 {
			return fmt.Errorf("usage: splashify profile picture <path-to-image>")
		}
		return cmdProfilePicture(args[1])

	case "2fa", "two-factor", "totp":
		return cmdProfile2FA(args[1:])

	default:
		return fmt.Errorf("unknown profile subcommand: %s\n"+
			"run: splashify profile  (or update | change-password | whatsapp | picture | 2fa)", sub)
	}
}

func cmdProfileUpdate(args []string) error {
	fs := flag.NewFlagSet("profile update", flag.ContinueOnError)
	first := fs.String("first-name", "", "first name (required)")
	last := fs.String("last-name", "", "last name (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *first == "" || *last == "" {
		return fmt.Errorf("usage: splashify profile update --first-name \"Alice\" --last-name \"Smith\"")
	}
	return runReq("PUT", "/app/profile", map[string]any{
		"first_name": *first,
		"last_name":  *last,
	})
}

func cmdProfileChangePassword(args []string) error {
	fs := flag.NewFlagSet("profile change-password", flag.ContinueOnError)
	current := fs.String("current", "", "current password (required)")
	newPw := fs.String("new", "", "new password (required, min 8 chars)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *current == "" || *newPw == "" {
		return fmt.Errorf("usage: splashify profile change-password --current <old> --new <new>")
	}
	return runReq("POST", "/app/profile/change-password", map[string]any{
		"current_password": *current,
		"new_password":     *newPw,
	})
}

func cmdProfileWhatsApp(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify profile whatsapp <send-otp|verify> …")
	}
	switch args[0] {
	case "send-otp", "send":
		fs := flag.NewFlagSet("profile whatsapp send-otp", flag.ContinueOnError)
		cc := fs.String("country-code", "+91", "country code with leading +")
		mobile := fs.String("mobile", "", "mobile number without country code (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *mobile == "" {
			return fmt.Errorf("usage: splashify profile whatsapp send-otp --country-code +91 --mobile 9876543210")
		}
		if !strings.HasPrefix(*cc, "+") {
			*cc = "+" + *cc
		}
		return runReq("POST", "/app/profile/whatsapp/send-otp", map[string]any{
			"country_code": *cc,
			"mobile":       *mobile,
		})

	case "verify":
		fs := flag.NewFlagSet("profile whatsapp verify", flag.ContinueOnError)
		cc := fs.String("country-code", "+91", "country code with leading +")
		mobile := fs.String("mobile", "", "mobile number without country code (required)")
		otp := fs.String("otp", "", "OTP delivered to the new number (required)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *mobile == "" || *otp == "" {
			return fmt.Errorf("usage: splashify profile whatsapp verify --country-code +91 --mobile 9876543210 --otp 123456")
		}
		if !strings.HasPrefix(*cc, "+") {
			*cc = "+" + *cc
		}
		return runReq("POST", "/app/profile/whatsapp/verify", map[string]any{
			"country_code": *cc,
			"mobile":       *mobile,
			"otp":          *otp,
		})

	default:
		return fmt.Errorf("unknown profile whatsapp subcommand: %s\nrun: splashify profile whatsapp", args[0])
	}
}

func cmdProfilePicture(filePath string) error {
	cfg, err := requireConfig()
	if err != nil {
		return err
	}
	raw, err := newAPIClient(cfg.BaseURL, cfg.Token).
		uploadFile("/app/profile/picture", "profile_picture", filePath)
	if err != nil {
		return err
	}
	printJSON(raw)
	return nil
}

func cmdProfile2FA(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: splashify profile 2fa <enable|disable>")
	}
	var enabled bool
	switch args[0] {
	case "enable", "on", "true":
		enabled = true
	case "disable", "off", "false":
		enabled = false
	default:
		return fmt.Errorf("usage: splashify profile 2fa <enable|disable>")
	}
	return runReq("POST", "/app/profile/2fa", map[string]any{"enabled": enabled})
}
