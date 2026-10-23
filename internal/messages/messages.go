// Package messages builds native structured WhatsApp messages after validation.
package messages

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// PollBuilder is optional on the send client, keeping media/download independent.
type PollBuilder interface {
	BuildPollCreation(string, []string, int) *waE2E.Message
}

var _ PollBuilder = (*whatsmeow.Client)(nil)

type Contact struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

func validText(value string, limit int, required bool) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && (!required || strings.TrimSpace(value) != "") && !strings.ContainsRune(value, 0)
}

func phone(value string) (string, error) {
	if user, server, hasServer := strings.Cut(value, "@"); hasServer {
		if server != "c.us" && server != "s.whatsapp.net" {
			return "", errors.New("invalid contact phone server")
		}
		value = user
	}
	value = strings.TrimPrefix(value, "+")
	if len(value) < 7 || len(value) > 15 {
		return "", errors.New("contact phone must contain 7 to 15 digits")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return "", errors.New("invalid contact phone")
		}
	}
	return value, nil
}

func escape(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", ";", "\\;", ",", "\\,").Replace(value)
}

// fold follows vCard's 75-octet line limit without splitting UTF-8 characters.
func fold(value string) string {
	var out strings.Builder
	column := 0
	for _, r := range value {
		n := utf8.RuneLen(r)
		if column+n > 75 {
			out.WriteString("\r\n ")
			column = 1
		}
		out.WriteRune(r)
		column += n
	}
	return out.String() + "\r\n"
}

// Contacts accepts either explicit cards or Node contactsId, never raw vCard.
// Missing names use the number; no remote contact/profile lookup is performed.
func Contacts(cards []Contact, ids []string, name string) (*waE2E.Message, error) {
	if len(cards) > 0 && len(ids) > 0 {
		return nil, errors.New("use contacts or contactsId, not both")
	}
	if !validText(name, 256, false) {
		return nil, errors.New("name must fit in 256 characters")
	}
	if len(ids) > 0 {
		for _, id := range ids {
			cards = append(cards, Contact{Name: name, Phone: id})
		}
	}
	if len(cards) == 0 || len(cards) > 20 {
		return nil, errors.New("provide between 1 and 20 contacts")
	}
	out := make([]*waE2E.ContactMessage, 0, len(cards))
	for _, card := range cards {
		number, err := phone(card.Phone)
		if err != nil {
			return nil, err
		}
		if card.Name == "" {
			card.Name = number
		}
		if !validText(card.Name, 256, true) {
			return nil, errors.New("contact name must contain 1 to 256 characters")
		}
		vcard := "BEGIN:VCARD\r\nVERSION:3.0\r\n" + fold("FN:"+escape(card.Name)) + fold("N:;"+escape(card.Name)+";;;") + fold("TEL;TYPE=CELL;waid="+number+":+"+number) + "END:VCARD\r\n"
		out = append(out, &waE2E.ContactMessage{DisplayName: proto.String(card.Name), Vcard: proto.String(vcard)})
	}
	if len(out) == 1 {
		return &waE2E.Message{ContactMessage: out[0]}, nil
	}
	return &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{DisplayName: proto.String("Contacts"), Contacts: out}}, nil
}

func coordinate(raw json.RawMessage, limit float64) (float64, error) {
	var text string
	if len(raw) == 0 || string(raw) == "null" {
		return 0, errors.New("lat and lng are required")
	}
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > limit {
		return 0, errors.New("invalid latitude or longitude")
	}
	return value, nil
}

func Location(lat, lng json.RawMessage, title, address string) (*waE2E.Message, error) {
	x, err := coordinate(lat, 90)
	if err != nil {
		return nil, err
	}
	y, err := coordinate(lng, 180)
	if err != nil {
		return nil, err
	}
	if !validText(title, 256, false) || !validText(address, 1024, false) {
		return nil, errors.New("title/address exceed 256/1024 characters")
	}
	return &waE2E.Message{LocationMessage: &waE2E.LocationMessage{DegreesLatitude: &x, DegreesLongitude: &y, Name: &title, Address: &address}}, nil
}

// Poll validation happens before calling the SDK, whose builder otherwise
// silently changes out-of-range selectable counts to zero (all options).
func ValidatePoll(name string, choices []string, selectable *int) (int, error) {
	if !validText(name, 255, true) || len(choices) < 2 || len(choices) > 12 {
		return 0, errors.New("poll requires a name (1..255 characters) and 2..12 choices")
	}
	seen := make([]string, 0, len(choices))
	for _, choice := range choices {
		key := strings.TrimSpace(choice)
		if !validText(choice, 100, true) {
			return 0, errors.New("poll choices must be distinct and contain 1..100 characters")
		}
		for _, previous := range seen {
			if strings.EqualFold(previous, key) {
				return 0, errors.New("poll choices must be distinct and contain 1..100 characters")
			}
		}
		seen = append(seen, key)
	}
	count := 1
	if selectable != nil {
		count = *selectable
	}
	if count < 0 || count > len(choices) {
		return 0, errors.New("selectableCount must be between zero (all choices) and the number of choices")
	}
	return count, nil
}
