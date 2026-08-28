package main

import (
	"encoding/json"
	"strings"
)

// Card shapes (CW-31).
//
// Two axes, deliberately kept apart:
//
//	Type     — the kind of work. Exactly one per ticket.
//	Sections — optional shaped blocks living in cardData. Any type may carry any
//	           section.
//
// UAT and scenarios are sections, not types. A story can have UAT and so can a
// defect; making them types would force a choice that does not exist and would
// leave no way to say "this defect has acceptance criteria too".
//
// The division of the record:
//
//	Title    — the heading.
//	Body     — evidence. Logs, snippets, stack traces, history.
//	cardData — shaped blocks the UI renders into the card content well.
//
// Empty blocks are hidden by the UI rather than rendered as blank chrome, so
// seeding an empty slot costs nothing visually but tells an agent the slot
// exists.

// Ticket types.
const (
	TicketTypeStory  = "story"
	TicketTypeDefect = "defect"
	TicketTypeSpike  = "spike"
	TicketTypeChore  = "chore"

	// Rows created before CW-31 carry "ticket". It stays valid on input and is
	// canonicalised to story on write; existing rows are never rewritten, so a
	// board created last week still reads correctly.
	TicketTypeLegacy = "ticket"
)

// Section keys inside cardData.
const (
	SectionStory      = "story"
	SectionAcceptance = "acceptance"
	SectionScenarios  = "scenarios"
	SectionUAT        = "uat"
	SectionDefect     = "defect"
	SectionSpike      = "spike"
	SectionChore      = "chore"
	SectionSource     = "source"
)

// Who shaped the card. Recorded so a reviewer can tell an agent's inference
// from something a human actually said.
const (
	SourceAgent = "agent"
	SourceUser  = "user"
)

// normalizeTicketType validates and canonicalises a ticket type.
//
// An empty type means story: most cards are stories, and forcing every caller
// to say so would be noise. Returns ok=false for anything outside the catalog
// rather than silently coercing it — a typo should fail loudly at the edge, not
// become a card nobody can find.
func normalizeTicketType(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", TicketTypeLegacy, TicketTypeStory:
		return TicketTypeStory, true
	case TicketTypeDefect:
		return TicketTypeDefect, true
	case TicketTypeSpike:
		return TicketTypeSpike, true
	case TicketTypeChore:
		return TicketTypeChore, true
	default:
		return "", false
	}
}

// seedSections are the blocks filled in for a type when the caller sent none.
// Nothing here seeds a story onto a chore or a UAT onto a spike: an empty block
// is an invitation, and inviting the wrong thing is how cards fill with
// ceremony nobody wanted.
var seedSections = map[string][]string{
	TicketTypeStory:  {SectionStory, SectionAcceptance},
	TicketTypeDefect: {SectionDefect},
	TicketTypeSpike:  {SectionSpike},
	TicketTypeChore:  {SectionChore},
}

// offerSections are blocks worth suggesting but never auto-created — they cost
// real thought to write, so an empty one is clutter rather than an invitation.
// They come back as "omitted" so the agent can offer them to the user.
//
// Spikes and chores offer nothing: a spike is a question with a timebox, and a
// chore is work that just needs doing. Neither earns a UAT.
var offerSections = map[string][]string{
	TicketTypeStory:  {SectionScenarios, SectionUAT},
	TicketTypeDefect: {SectionScenarios, SectionUAT},
	TicketTypeSpike:  {},
	TicketTypeChore:  {},
}

// emptySection returns the blank shape for a section key.
func emptySection(key string) any {
	switch key {
	case SectionStory:
		return map[string]any{"asA": "", "iWant": "", "soThat": ""}
	case SectionAcceptance:
		return []any{}
	case SectionScenarios:
		return []any{}
	case SectionUAT:
		return []any{}
	case SectionDefect:
		return map[string]any{"repro": "", "expected": "", "actual": ""}
	case SectionSpike:
		return map[string]any{
			"question": "", "timeboxMinutes": 0, "approach": "",
			"findings": "", "outcome": "", "followUp": "",
		}
	case SectionChore:
		return map[string]any{"why": "", "doneWhen": ""}
	default:
		return map[string]any{}
	}
}

// defaultCardSchema is seeded onto NEW boards so a client can discover the
// catalog without hardcoding it. Existing boards keep whatever they have —
// rewriting a board's schema underneath it would change how its cards render.
func defaultCardSchema() []byte {
	schema := map[string]any{
		"version": 1,
		"types":   []string{TicketTypeStory, TicketTypeDefect, TicketTypeSpike, TicketTypeChore},
		"sections": map[string]any{
			SectionStory:      emptySection(SectionStory),
			SectionAcceptance: emptySection(SectionAcceptance),
			SectionScenarios:  []any{map[string]any{"name": "", "given": "", "when": "", "then": ""}},
			SectionUAT:        emptySection(SectionUAT),
			SectionDefect:     emptySection(SectionDefect),
			SectionSpike:      emptySection(SectionSpike),
			SectionChore:      emptySection(SectionChore),
		},
		"seed":  seedSections,
		"offer": offerSections,
	}
	b, err := json.Marshal(schema)
	if err != nil {
		// The literal above is fixed and cannot fail to marshal; an empty
		// schema is still a valid board rather than a failed create.
		return []byte(`{}`)
	}
	return b
}

// sectionFilled reports whether a section carries anything a reader would see.
// An empty string, empty list or all-blank object counts as unfilled, so a
// seeded slot is not reported back as though the agent had written something.
func sectionFilled(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(t) != ""
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case []any:
		for _, item := range t {
			if sectionFilled(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range t {
			if sectionFilled(item) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// shapeCardData builds the cardData stored on a new ticket (CW-32).
//
// It returns the merged object plus the two lists the tool result reports:
//
//	shaped  — sections that are present and carry content
//	omitted — sections worth offering that are not there yet
//
// Caller-supplied keys always win, and unknown keys are carried through
// untouched: the catalog describes what we render, not what may be stored.
func shapeCardData(ticketType, title string, provided map[string]any, seed bool) (map[string]any, []string, []string) {
	out := map[string]any{}
	for k, v := range provided {
		out[k] = v
	}

	if seed {
		seeded := false
		for _, key := range seedSections[ticketType] {
			if _, ok := out[key]; ok {
				continue
			}
			out[key] = emptySection(key)
			seeded = true
		}

		// A spike's question is the one slot we can fill honestly from what the
		// caller already gave us: the title IS the question being asked.
		if ticketType == TicketTypeSpike {
			if spike, ok := out[SectionSpike].(map[string]any); ok {
				if q, _ := spike["question"].(string); strings.TrimSpace(q) == "" {
					spike["question"] = strings.TrimSpace(title)
					seeded = true
				}
			}
		}

		// Record that a machine shaped this, unless the caller said otherwise.
		if seeded {
			if _, ok := out[SectionSource]; !ok {
				out[SectionSource] = SourceAgent
			}
		}
	}

	shaped := []string{}
	for _, key := range catalogOrder {
		if v, ok := out[key]; ok && sectionFilled(v) {
			shaped = append(shaped, key)
		}
	}

	omitted := []string{}
	for _, key := range offerSections[ticketType] {
		if _, ok := out[key]; !ok {
			omitted = append(omitted, key)
		}
	}

	return out, shaped, omitted
}

// catalogOrder fixes the order sections are reported in, so two identical
// creates do not produce differently-ordered result envelopes.
var catalogOrder = []string{
	SectionStory, SectionAcceptance, SectionScenarios, SectionUAT,
	SectionDefect, SectionSpike, SectionChore,
}

// mergeCardData applies an update's cardData over what is already stored:
// provided keys replace, omitted keys stay.
//
// A whole-object replace would mean an agent adding a UAT silently destroyed
// the story block it did not mention — the failure would land in the UI long
// after the call that caused it.
func mergeCardData(existing, incoming []byte) []byte {
	if len(incoming) == 0 {
		return existing
	}
	var in map[string]any
	if err := json.Unmarshal(incoming, &in); err != nil {
		// Not an object (an array, or malformed). Nothing sensible to merge
		// into, so honour the caller's bytes rather than guessing.
		return incoming
	}
	var cur map[string]any
	if err := json.Unmarshal(existing, &cur); err != nil || cur == nil {
		cur = map[string]any{}
	}
	for k, v := range in {
		cur[k] = v
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return incoming
	}
	return b
}

// shapeHint is the one-line nudge on a create result. It names where shapes
// live and how to add what is missing, so an agent does not have to guess at a
// second tool.
//
// CW-35 decided hint-only over syncing cardData.uat into a checklist: a one-way
// sync between two mutable stores diverges the moment someone ticks a box, and
// nothing reconciles them afterwards. cardData.uat stays the text; a checklist
// is created deliberately when a human actually wants to tick things off. That
// makes this hint the whole feature, so it points at create_checklist in both
// directions — when a UAT is missing, and when one exists and is worth ticking.
func shapeHint(shaped, omitted []string) string {
	uatPresent := false
	for _, s := range shaped {
		if s == SectionUAT {
			uatPresent = true
			break
		}
	}

	switch {
	case len(omitted) > 0:
		return "Shapes live on cardData. Add " + strings.Join(omitted, " or ") +
			" with update_ticket, or create_checklist title=UAT for something a human ticks off."
	case uatPresent:
		return "Shapes live on cardData. This card has a UAT — create_checklist title=UAT if someone should tick it off."
	default:
		return "Shapes live on cardData. Call update_ticket with cardData to refine them."
	}
}
