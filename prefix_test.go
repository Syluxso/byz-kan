package main

import "testing"

func TestDeriveKeyPrefixFromName(t *testing.T) {
	cases := map[string]string{
		"Shipping":      "SHIP",
		"AB":            "AB",
		"A":             "BOARD",
		"":              "BOARD",
		"ship-42 board": "SHIP",
		"12ab":          "12AB",
	}
	for in, want := range cases {
		if got := deriveKeyPrefixFromName(in); got != want {
			t.Errorf("deriveKeyPrefixFromName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeKeyPrefix(t *testing.T) {
	got, err := normalizeKeyPrefix(" ship_1 ")
	if err != nil || got != "SHIP1" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := normalizeKeyPrefix("x"); err != errInvalid {
		t.Fatalf("expected invalid, got %v", err)
	}
}

func TestTicketKey(t *testing.T) {
	if ticketKey("ship", 42) != "SHIP-42" {
		t.Fatal(ticketKey("ship", 42))
	}
}

func TestIsUUID(t *testing.T) {
	if !isUUID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") {
		t.Fatal("valid uuid rejected")
	}
	if isUUID("nope") {
		t.Fatal("invalid uuid accepted")
	}
}

func TestTimeMinutesFromRange(t *testing.T) {
	m := minutesFromRange(
		mustParseTime("2026-08-24T10:00:00Z"),
		mustParseTime("2026-08-24T10:01:01Z"),
	)
	if m != 2 {
		t.Fatalf("got %d want 2 (ceil 61s)", m)
	}
}
