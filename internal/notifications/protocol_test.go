package notifications

import (
	"encoding/json"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"strings"
	"testing"
)

func TestSafeReceiptsAndCallMetadata(t *testing.T) {
	jid := types.NewJID("123", types.DefaultUserServer)
	event, data := Protocol(&events.Receipt{MessageSource: types.MessageSource{Chat: jid, Sender: jid}, MessageIDs: []types.MessageID{"one"}, Type: types.ReceiptTypeRead})
	if event != "onack" || len(data["messageIds"].([]types.MessageID)) != 1 {
		t.Fatalf("receipt %#v", data)
	}
	event, data = Protocol(&events.CallOffer{BasicCallMeta: types.BasicCallMeta{CallID: "call", From: jid}})
	raw, _ := json.Marshal(data)
	if event != "oncall" || !strings.Contains(string(raw), "call") || strings.Contains(string(raw), "key") {
		t.Fatalf("unsafe call %s", raw)
	}
	if _, data = Protocol((*events.Receipt)(nil)); data != nil {
		t.Fatal("nil event emitted")
	}
	if _, data = Protocol(struct{ Secret string }{"private"}); data != nil {
		t.Fatal("unknown protocol object exposed")
	}
}
