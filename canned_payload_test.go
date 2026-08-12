package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustJSON decodes a payload literal the way the API would hand it to us
// (map[string]any with float64 numbers), so these tests exercise the same
// value shapes the real code sees.
func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}
	return v
}

// The reported case. This expected string is duplicated verbatim in the
// app-side check for app/lib/canned-message-payload.ts — the two
// implementations must not drift.
func TestPlanCannedSendInteractiveList(t *testing.T) {
	payload := mustJSON(t, `{
	  "interactive": {
	    "type": "list",
	    "header": {"type": "text", "text": "Support"},
	    "body": {"text": "How can we help you today?"},
	    "footer": {"text": "Available 9am-6pm IST"},
	    "action": {
	      "button": "View options",
	      "sections": [
	        {"title": "Billing", "rows": [
	          {"id": "inv", "title": "Invoices", "description": "Download past invoices"},
	          {"id": "pay", "title": "Make a payment"}
	        ]},
	        {"title": "Technical", "rows": [
	          {"id": "api", "title": "API help", "description": "Keys, limits, webhooks"}
	        ]}
	      ]
	    }
	  }
	}`)

	plan, err := planCannedSend("INTERACTIVE_LIST", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Endpoint != "/app/messages/send-text" {
		t.Errorf("endpoint = %q, want /app/messages/send-text", plan.Endpoint)
	}

	want := strings.Join([]string{
		"*Support*",
		"",
		"How can we help you today?",
		"",
		"_Billing_",
		"1. Invoices — Download past invoices",
		"2. Make a payment",
		"",
		"_Technical_",
		"3. API help — Keys, limits, webhooks",
		"",
		"Available 9am-6pm IST",
		"",
		"Reply with the number of your choice.",
	}, "\n")

	got, _ := plan.Body["text"].(string)
	if got != want {
		t.Errorf("flattened text mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestPlanCannedSendSingleSectionHasNoHeading(t *testing.T) {
	payload := mustJSON(t, `{"interactive":{"type":"list","body":{"text":"Pick one:"},
	  "action":{"sections":[{"title":"Menu","rows":[
	    {"id":"a","title":"Alpha"},{"id":"b","title":"Beta"}]}]}}}`)

	plan, err := planCannedSend("INTERACTIVE_LIST", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Pick one:\n\n1. Alpha\n2. Beta\n\nReply with the number of your choice."
	if got, _ := plan.Body["text"].(string); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPlanCannedSendButtonsAndCTA(t *testing.T) {
	buttons := mustJSON(t, `{"interactive":{"type":"button","body":{"text":"Confirm your order?"},
	  "action":{"buttons":[{"type":"reply","reply":{"id":"y","title":"Yes"}},
	                       {"type":"reply","reply":{"id":"n","title":"No"}}]}}}`)
	plan, err := planCannedSend("INTERACTIVE_BUTTON", buttons)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Confirm your order?\n\n1. Yes\n2. No\n\nReply with the number of your choice."
	if got, _ := plan.Body["text"].(string); got != want {
		t.Errorf("buttons: got %q, want %q", got, want)
	}

	cta := mustJSON(t, `{"interactive":{"type":"cta_url","body":{"text":"Track your shipment:"},
	  "action":{"parameters":{"display_text":"Track order","url":"https://ex.com/t/9"}}}}`)
	plan, err = planCannedSend("INTERACTIVE_CTA_URL", cta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The URL must survive — a bare display_text would strand the recipient.
	wantCTA := "Track your shipment:\n\nTrack order: https://ex.com/t/9"
	if got, _ := plan.Body["text"].(string); got != wantCTA {
		t.Errorf("cta: got %q, want %q", got, wantCTA)
	}
}

// CONTACT must reach send-contact with contacts[] intact. Before this it fell
// through to a stringified payload.
func TestPlanCannedSendContact(t *testing.T) {
	payload := mustJSON(t, `{"contacts":[{"name":{"formatted_name":"Asha Rao"},
	  "phones":[{"phone":"+919876543210","type":"WORK"}]}]}`)

	plan, err := planCannedSend("CONTACT", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Endpoint != "/app/messages/send-contact" {
		t.Fatalf("endpoint = %q, want /app/messages/send-contact", plan.Endpoint)
	}
	contacts, ok := plan.Body["contacts"].([]any)
	if !ok || len(contacts) != 1 {
		t.Fatalf("contacts = %#v, want 1 entry", plan.Body["contacts"])
	}
	if name := asMap(asMap(contacts[0])["name"])["formatted_name"]; name != "Asha Rao" {
		t.Errorf("formatted_name = %v, want Asha Rao", name)
	}
}

func TestPlanCannedSendMediaAndLocation(t *testing.T) {
	doc := mustJSON(t, `{"document":{"link":"https://x/a.pdf","filename":"a.pdf","caption":"Menu"}}`)
	plan, err := planCannedSend("DOCUMENT", doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Body["media_url"] != "https://x/a.pdf" || plan.Body["filename"] != "a.pdf" || plan.Body["caption"] != "Menu" {
		t.Errorf("document body = %#v", plan.Body)
	}

	// .url is accepted alongside .link — payloads in the wild use both.
	img := mustJSON(t, `{"image":{"url":"https://x/i.jpg"}}`)
	if plan, err = planCannedSend("IMAGE", img); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if plan.Body["media_url"] != "https://x/i.jpg" {
		t.Errorf("image media_url = %v", plan.Body["media_url"])
	}

	// Numeric strings coerce; missing coords must fail rather than send 0,0.
	loc := mustJSON(t, `{"location":{"latitude":"12.97","longitude":"77.59","name":"HQ"}}`)
	if plan, err = planCannedSend("LOCATION", loc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if plan.Body["latitude"] != 12.97 || plan.Body["longitude"] != 77.59 {
		t.Errorf("location body = %#v", plan.Body)
	}
	if _, err = planCannedSend("LOCATION", mustJSON(t, `{"location":{}}`)); err == nil {
		t.Error("expected an error for a location with no coordinates")
	}
}

func TestPlanCannedSendAddressRendersLines(t *testing.T) {
	payload := mustJSON(t, `{"address":{"name":"Warehouse","address":"12 MG Rd",
	  "city":"Bengaluru","state":"KA","zip":"560001","country":"India"}}`)
	plan, err := planCannedSend("ADDRESS", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Warehouse\n12 MG Rd\nBengaluru, KA\n560001 India"
	if got, _ := plan.Body["text"].(string); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Nothing unrenderable may be turned into an outgoing message.
func TestPlanCannedSendRefusesUnrenderable(t *testing.T) {
	cases := []struct {
		name    string
		msgType string
		payload string
	}{
		{"empty interactive", "INTERACTIVE_LIST", `{"interactive":{"type":"list","action":{}}}`},
		{"contact with none", "CONTACT", `{"contacts":[]}`},
		{"text with no body", "TEXT", `{"text":{}}`},
		{"media with no url", "IMAGE", `{"image":{}}`},
		{"unknown type", "CAROUSEL", `{"foo":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := planCannedSend(tc.msgType, mustJSON(t, tc.payload)); err == nil {
				t.Errorf("expected an error for %s", tc.msgType)
			}
		})
	}
}

// validateCannedPayload is what keeps a type/payload mismatch out of the
// account — `--payload` was previously only checked for being valid JSON.
func TestValidateCannedPayloadRejectsMismatch(t *testing.T) {
	// Valid JSON, valid TEXT payload, but declared INTERACTIVE_LIST.
	err := validateCannedPayload("INTERACTIVE_LIST", mustJSON(t, `{"text":{"body":"hi"}}`))
	if err == nil {
		t.Fatal("expected a mismatch error")
	}
	if !strings.Contains(err.Error(), "INTERACTIVE_LIST") {
		t.Errorf("error should name the declared type, got: %v", err)
	}

	// A matching payload passes.
	if err := validateCannedPayload("TEXT", mustJSON(t, `{"text":{"body":"hi"}}`)); err != nil {
		t.Errorf("well-formed TEXT payload rejected: %v", err)
	}
}

// Every type the CLI accepts at create time must be plannable from a
// well-formed payload — otherwise `canned create` can still write a record
// that `canned send` refuses.
func TestEveryAcceptedTypeIsSendable(t *testing.T) {
	fixtures := map[string]string{
		"TEXT":                `{"text":{"body":"hi"}}`,
		"IMAGE":               `{"image":{"link":"https://x/i.jpg"}}`,
		"VIDEO":               `{"video":{"link":"https://x/v.mp4"}}`,
		"AUDIO":               `{"audio":{"link":"https://x/a.mp3"}}`,
		"DOCUMENT":            `{"document":{"link":"https://x/d.pdf"}}`,
		"LOCATION":            `{"location":{"latitude":12.97,"longitude":77.59}}`,
		"CONTACT":             `{"contacts":[{"name":{"formatted_name":"A"}}]}`,
		"INTERACTIVE_BUTTON":  `{"interactive":{"body":{"text":"?"},"action":{"buttons":[{"reply":{"title":"Yes"}}]}}}`,
		"INTERACTIVE_LIST":    `{"interactive":{"body":{"text":"?"},"action":{"sections":[{"rows":[{"title":"A"}]}]}}}`,
		"INTERACTIVE_CTA_URL": `{"interactive":{"body":{"text":"?"},"action":{"parameters":{"url":"https://x"}}}}`,
		"ADDRESS":             `{"address":{"name":"W","city":"Bengaluru"}}`,
	}
	for mt := range cannedMessageTypes {
		fixture, ok := fixtures[mt]
		if !ok {
			t.Errorf("cannedMessageTypes accepts %s but no send fixture covers it — "+
				"either add the fixture or stop accepting the type", mt)
			continue
		}
		if _, err := planCannedSend(mt, mustJSON(t, fixture)); err != nil {
			t.Errorf("%s: accepted at create but unsendable: %v", mt, err)
		}
	}
}
