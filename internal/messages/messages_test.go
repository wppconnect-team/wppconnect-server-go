package messages

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
)

func TestContactsEscapingAndFolding(t *testing.T) {
	name := strings.Repeat("João", 30) + "\r\nTEL:injected;comma,back\\slash"
	msg, err := Contacts([]Contact{{Name: name, Phone: "+5511999999999"}}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	vcard := msg.GetContactMessage().GetVcard()
	if strings.Contains(vcard, "\r\nTEL:injected") || !strings.Contains(strings.ReplaceAll(vcard, "\r\n ", ""), "\\nTEL:injected\\;comma\\,back\\\\slash") {
		t.Fatalf("vcard escaping: %q", vcard)
	}
	for _, line := range strings.Split(vcard, "\r\n") {
		if len(line) > 75 || !utf8.ValidString(line) {
			t.Fatal("invalid folded line")
		}
	}
	multi, err := Contacts(nil, []string{"5511999999999@c.us", "5521999999999"}, "")
	if err != nil || len(multi.GetContactsArrayMessage().GetContacts()) != 2 {
		t.Fatalf("multi: %v", err)
	}
	if multi.GetContactsArrayMessage().GetContacts()[0].GetDisplayName() != "5511999999999" {
		t.Fatal("name fallback")
	}
	for _, cards := range [][]Contact{nil, {{Phone: "abc"}}, {{Phone: "5511999999999", Name: strings.Repeat("x", 257)}}, {{Phone: "5511999999999\r\nFN:evil"}}} {
		if _, err := Contacts(cards, nil, ""); err == nil {
			t.Fatal("accepted invalid card")
		}
	}
	if _, err := Contacts([]Contact{{Phone: "5511999999999"}}, []string{"5511999999999"}, ""); err == nil {
		t.Fatal("accepted ambiguous cards")
	}
}

func TestLocationBoundsAndZero(t *testing.T) {
	msg, err := Location(json.RawMessage(`0`), json.RawMessage(`"-180"`), "Origin", "")
	if err != nil || msg.GetLocationMessage().GetDegreesLatitude() != 0 || msg.GetLocationMessage().GetDegreesLongitude() != -180 {
		t.Fatalf("coordinates: %v", err)
	}
	for _, tc := range [][2]string{{"", `0`}, {`null`, `0`}, {`91`, `0`}, {`0`, `181`}, {`"NaN"`, `0`}, {`"Inf"`, `0`}, {`true`, `0`}, {`{}`, `0`}} {
		if _, err := Location(json.RawMessage(tc[0]), json.RawMessage(tc[1]), "", ""); err == nil {
			t.Fatalf("accepted %v", tc)
		}
	}
}

func TestPollValidationAndSDKSecrets(t *testing.T) {
	choices := []string{"Yes", "No"}
	for _, count := range []int{-1, 3} {
		if _, err := ValidatePoll("Question", choices, &count); err == nil {
			t.Fatal("accepted count")
		}
	}
	for _, options := range [][]string{{"only"}, {"yes", " YES "}, {"yes", " "}, {strings.Repeat("x", 101), "no"}} {
		if _, err := ValidatePoll("Question", options, nil); err == nil {
			t.Fatal("accepted options")
		}
	}
	zero := 0
	if count, err := ValidatePoll("Question", choices, &zero); err != nil || count != 0 {
		t.Fatal("zero means all")
	}
	if count, err := ValidatePoll("Question", choices, nil); err != nil || count != 1 {
		t.Fatal("default selectable")
	}
	client := &whatsmeow.Client{}
	a, b := client.BuildPollCreation("Question", choices, 1), client.BuildPollCreation("Question", choices, 1)
	if len(a.GetMessageContextInfo().GetMessageSecret()) != 32 || string(a.GetMessageContextInfo().GetMessageSecret()) == string(b.GetMessageContextInfo().GetMessageSecret()) {
		t.Fatal("poll secrets reused")
	}
}
