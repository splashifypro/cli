package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Canned-message payload → concrete send plan.
//
// This is the Go twin of app/lib/canned-message-payload.ts and must stay
// behaviour-identical: the same stored payload has to produce the same
// outgoing message whether it is sent from the app composer or from
// `splashify canned send`. canned_payload_test.go pins the shared cases.
//
// Why it exists on both sides: `canned create --type` accepts all 11
// CannedMessageType values and the backend validates none of them, but the
// outbound surface is only /app/messages/{send-text,send-media,send-template,
// send-location,send-contact,send-reaction,typing-indicator}. There is no
// free-form interactive send, so interactive buttons ride approved templates
// or they do not exist.
//
// The numbered text list below is therefore the design, not a degradation —
// an interactive payload is flattened into something the recipient can act on
// by replying with a number. Two rules follow:
//
//  1. Never emit a raw payload to a customer. A payload we cannot render is a
//     bug to report, not text to send.
//  2. Never silently drop the interaction. A list whose rows vanish is worse
//     than a refusal.

// cannedSendPlan is the resolved endpoint + request body (minus "to") for a
// stored canned message.
type cannedSendPlan struct {
	Kind     string // text | media | location | contact
	Endpoint string
	Body     map[string]any
}

// ─── small JSON accessors ────────────────────────────────────────────────────

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// textOf returns a trimmed string for string values, "" for everything else.
func textOf(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// numOf coerces JSON numbers and numeric strings; ok is false when neither.
func numOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// ─── interactive → numbered text list ────────────────────────────────────────

// flattenInteractiveToText renders a WhatsApp `interactive` object as the
// numbered text list. Options are numbered continuously across sections so
// "reply 4" is unambiguous in a grouped list; section titles become
// subheadings and are not numbered.
func flattenInteractiveToText(interactive any) (string, error) {
	node := asMap(interactive)
	action := asMap(node["action"])
	var blocks []string

	// A header may be text | image | video | document — only a text header has
	// a textual equivalent; media headers are dropped rather than misrepresented.
	header := asMap(node["header"])
	if t, _ := header["type"].(string); t == "text" {
		if h := textOf(header["text"]); h != "" {
			blocks = append(blocks, "*"+h+"*")
		}
	}

	if body := textOf(asMap(node["body"])["text"]); body != "" {
		blocks = append(blocks, body)
	}

	// Collect actionable options in document order.
	var options []string

	// type "list" → action.sections[].rows[]
	sections := asSlice(action["sections"])
	for _, rawSection := range sections {
		section := asMap(rawSection)
		// Only label a section when the list is actually grouped — a lone
		// titled section reads as a redundant heading.
		if title := textOf(section["title"]); title != "" && len(sections) > 1 {
			options = append(options, "\n_"+title+"_")
		}
		for _, rawRow := range asSlice(section["rows"]) {
			row := asMap(rawRow)
			rowTitle := textOf(row["title"])
			if rowTitle == "" {
				continue
			}
			if desc := textOf(row["description"]); desc != "" {
				options = append(options, rowTitle+" — "+desc)
			} else {
				options = append(options, rowTitle)
			}
		}
	}

	// type "button" → action.buttons[].reply.title
	for _, rawButton := range asSlice(action["buttons"]) {
		button := asMap(rawButton)
		title := textOf(asMap(button["reply"])["title"])
		if title == "" {
			title = textOf(button["title"])
		}
		if title != "" {
			options = append(options, title)
		}
	}

	if len(options) > 0 {
		n := 0
		lines := make([]string, 0, len(options))
		for _, opt := range options {
			if strings.HasPrefix(opt, "\n_") {
				lines = append(lines, opt)
				continue
			}
			n++
			lines = append(lines, fmt.Sprintf("%d. %s", n, opt))
		}
		blocks = append(blocks, strings.TrimSpace(strings.Join(lines, "\n")))
	}

	// type "cta_url" → action.parameters{display_text,url}. The URL has to land
	// in the body; a bare display_text would strand the recipient.
	params := asMap(action["parameters"])
	if u := textOf(params["url"]); u != "" {
		if label := textOf(params["display_text"]); label != "" {
			blocks = append(blocks, label+": "+u)
		} else {
			blocks = append(blocks, u)
		}
	}

	if footer := textOf(asMap(node["footer"])["text"]); footer != "" {
		blocks = append(blocks, footer)
	}

	// The reply hint is what makes the numbered list an interaction rather than
	// a wall of text. Only earned when there is something to pick.
	if len(options) > 0 {
		blocks = append(blocks, "Reply with the number of your choice.")
	}

	out := strings.TrimSpace(strings.Join(blocks, "\n\n"))
	if out == "" {
		return "", fmt.Errorf("this interactive canned message has no text, options, or link to send")
	}
	return out, nil
}

// flattenAddressToText renders an ADDRESS payload as readable lines. Shape
// varies: WhatsApp's address_message nests under `interactive`, while
// CLI-authored payloads tend to use a flat `address` object.
func flattenAddressToText(payload map[string]any) (string, error) {
	if iv, ok := payload["interactive"]; ok {
		return flattenInteractiveToText(iv)
	}

	addr := asMap(payload["address"])
	if len(addr) == 0 {
		addr = payload
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v := textOf(addr[k]); v != "" {
				return v
			}
		}
		return ""
	}
	joinNonEmpty := func(sep string, parts ...string) string {
		kept := parts[:0:0]
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, sep)
	}

	candidates := []string{
		pick("name"),
		pick("address", "street"),
		joinNonEmpty(", ", pick("city"), pick("state")),
		joinNonEmpty(" ", pick("zip", "postal_code"), pick("country")),
		pick("phone_number", "phone"),
	}
	lines := candidates[:0:0]
	for _, c := range candidates {
		if c != "" {
			lines = append(lines, c)
		}
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("this address canned message has no address fields to send")
	}
	return strings.Join(lines, "\n"), nil
}

// ─── planner ─────────────────────────────────────────────────────────────────

// planCannedSend maps a stored canned message onto the send endpoint that can
// carry it. The returned Body omits "to" — the caller adds the recipient.
//
// An error here means the record is unsendable as stored; callers surface it
// and send nothing rather than shipping a raw payload to a customer.
func planCannedSend(msgType string, payload any) (cannedSendPlan, error) {
	p := asMap(payload)
	mt := strings.ToUpper(strings.TrimSpace(msgType))

	switch mt {
	case "TEXT":
		body := textOf(asMap(p["text"])["body"])
		if body == "" {
			return cannedSendPlan{}, fmt.Errorf("this canned message has no text to send")
		}
		return cannedSendPlan{
			Kind:     "text",
			Endpoint: "/app/messages/send-text",
			Body:     map[string]any{"text": body},
		}, nil

	case "IMAGE", "VIDEO", "AUDIO", "DOCUMENT":
		key := strings.ToLower(mt)
		media := asMap(p[key])
		url := textOf(media["link"])
		if url == "" {
			url = textOf(media["url"])
		}
		if url == "" {
			return cannedSendPlan{}, fmt.Errorf("this %s canned message has no media URL", key)
		}
		body := map[string]any{"media_type": key, "media_url": url}
		if c := textOf(media["caption"]); c != "" {
			body["caption"] = c
		}
		if f := textOf(media["filename"]); f != "" {
			body["filename"] = f
		}
		return cannedSendPlan{Kind: "media", Endpoint: "/app/messages/send-media", Body: body}, nil

	case "LOCATION":
		loc := asMap(p["location"])
		lat, latOK := numOf(loc["latitude"])
		lng, lngOK := numOf(loc["longitude"])
		if !latOK || !lngOK {
			return cannedSendPlan{}, fmt.Errorf("this location canned message has no valid coordinates")
		}
		body := map[string]any{"latitude": lat, "longitude": lng}
		if n := textOf(loc["name"]); n != "" {
			body["name"] = n
		}
		if a := textOf(loc["address"]); a != "" {
			body["address"] = a
		}
		return cannedSendPlan{Kind: "location", Endpoint: "/app/messages/send-location", Body: body}, nil

	case "CONTACT":
		// send-contact takes Meta's canonical contacts[] verbatim.
		var contacts []any
		switch c := p["contacts"].(type) {
		case []any:
			contacts = c
		case map[string]any:
			contacts = []any{c}
		}
		if len(contacts) == 0 {
			return cannedSendPlan{}, fmt.Errorf("this contact canned message has no contacts")
		}
		return cannedSendPlan{
			Kind:     "contact",
			Endpoint: "/app/messages/send-contact",
			Body:     map[string]any{"contacts": contacts},
		}, nil

	case "INTERACTIVE_BUTTON", "INTERACTIVE_LIST", "INTERACTIVE_CTA_URL":
		// No interactive send endpoint exists — the numbered text list is the
		// interaction floor by design, not a stopgap.
		text, err := flattenInteractiveToText(p["interactive"])
		if err != nil {
			return cannedSendPlan{}, err
		}
		return cannedSendPlan{
			Kind:     "text",
			Endpoint: "/app/messages/send-text",
			Body:     map[string]any{"text": text},
		}, nil

	case "ADDRESS":
		text, err := flattenAddressToText(p)
		if err != nil {
			return cannedSendPlan{}, err
		}
		return cannedSendPlan{
			Kind:     "text",
			Endpoint: "/app/messages/send-text",
			Body:     map[string]any{"text": text},
		}, nil

	default:
		return cannedSendPlan{}, fmt.Errorf("unsupported canned message type %q", msgType)
	}
}

// validateCannedPayload rejects a payload that does not match its declared
// type at authoring time.
//
// `--payload` is otherwise only checked for being valid JSON, so
// `--type INTERACTIVE_LIST --payload '{"text":{"body":"hi"}}'` used to store a
// record that no sender could ever render. Failing here keeps the malformed
// row out of the account instead of surfacing as an error at send time.
func validateCannedPayload(msgType string, payload any) error {
	if _, err := planCannedSend(msgType, payload); err != nil {
		return fmt.Errorf("--payload doesn't match --type %s: %w", strings.ToUpper(strings.TrimSpace(msgType)), err)
	}
	return nil
}
