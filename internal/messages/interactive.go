package messages

import (
	"errors"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

type Choice struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Description string `json:"description"`
}

// Interactive constructs bounded reply choices. WhatsApp client/account support
// varies; these legacy protocol forms are explicitly experimental at HTTP level.
func Interactive(kind, title, body, footer, button string, choices []Choice) (*waE2E.Message, error) {
	limit := 10
	if kind == "send-buttons" {
		limit = 3
	}
	if !validText(body, 1024, true) || !validText(title, 60, false) || !validText(footer, 60, false) || len(choices) < 1 || len(choices) > limit {
		return nil, errors.New("invalid interactive text or choice count")
	}
	seen := map[string]bool{}
	for _, choice := range choices {
		if !validText(choice.ID, 128, true) || !validText(choice.Text, 20, true) || !validText(choice.Description, 72, false) || seen[choice.ID] {
			return nil, errors.New("interactive choices need distinct IDs (1..128), text (1..20), description up to 72 characters")
		}
		seen[choice.ID] = true
	}
	if kind == "send-buttons" {
		buttons := make([]*waE2E.ButtonsMessage_Button, len(choices))
		for i, v := range choices {
			buttons[i] = &waE2E.ButtonsMessage_Button{ButtonID: proto.String(v.ID), ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String(v.Text)}, Type: waE2E.ButtonsMessage_Button_RESPONSE.Enum()}
		}
		message := &waE2E.ButtonsMessage{ContentText: proto.String(body), FooterText: proto.String(footer), Buttons: buttons, HeaderType: waE2E.ButtonsMessage_EMPTY.Enum()}
		if title != "" {
			message.Header = &waE2E.ButtonsMessage_Text{Text: title}
			message.HeaderType = waE2E.ButtonsMessage_TEXT.Enum()
		}
		return &waE2E.Message{ButtonsMessage: message}, nil
	}
	if !validText(button, 20, true) {
		return nil, errors.New("list buttonText must contain 1..20 characters")
	}
	rows := make([]*waE2E.ListMessage_Row, len(choices))
	for i, v := range choices {
		rows[i] = &waE2E.ListMessage_Row{RowID: proto.String(v.ID), Title: proto.String(v.Text), Description: proto.String(v.Description)}
	}
	return &waE2E.Message{ListMessage: &waE2E.ListMessage{Title: proto.String(title), Description: proto.String(body), FooterText: proto.String(footer), ButtonText: proto.String(button), ListType: waE2E.ListMessage_SINGLE_SELECT.Enum(), Sections: []*waE2E.ListMessage_Section{{Title: proto.String("Options"), Rows: rows}}}}, nil
}
