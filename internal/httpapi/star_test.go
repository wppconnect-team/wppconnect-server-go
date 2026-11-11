package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/account"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/history"
	"github.com/wppconnect-team/wppconnect-server-go/internal/sendledger"
	"google.golang.org/protobuf/proto"
)

type starAccount struct {
	account.SDK
	patches []appstate.PatchInfo
	failure error
}

func (a *starAccount) SendAppState(ctx context.Context, p appstate.PatchInfo) error {
	a.patches = append(a.patches, p)
	return a.failure
}

func TestStarMessageContracts(t *testing.T) {
	h := newMediaHarness(t)
	a := &starAccount{}
	stores := map[string]*history.Store{}
	for _, name := range []string{"alpha", "beta"} {
		st, e := history.Open(filepath.Join(t.TempDir(), "h.db"), history.DefaultOptions())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { st.Close() })
		stores[name] = st
	}
	ledger, e := sendledger.Open(filepath.Join(t.TempDir(), "r.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer ledger.Close()
	api := (&Server{cfg: config.Config{SecretKey: "test-secret"}, sends: ledger, accountSession: func(string) (account.Client, bool) { return a, true }, historySession: func(n string) (*history.Store, func(), error) { return stores[n], func() {}, nil }}).router()
	group := types.NewJID("12345", types.GroupServer)
	sender := types.NewJID("123", types.DefaultUserServer)
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: group, Sender: sender, IsGroup: true}, ID: "msg", Timestamp: time.Now()}
	if e = stores["alpha"].Put(context.Background(), info, &waE2E.Message{Conversation: proto.String("hello")}); e != nil {
		t.Fatal(e)
	}
	call := func(session, op, body, key string, code int) string {
		t.Helper()
		method := "POST"
		if strings.HasSuffix(op, "result") {
			method = "GET"
		}
		r := httptest.NewRequest(method, "/api/"+session+"/"+op, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+h.tokens[session])
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("%s: %d %s", op, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	target := `{"chatId":"12345@g.us","messageId":"msg"`
	on := target + `,"star":true}`
	off := target + `,"star":false}`
	call("alpha", "star-message", off, "", 400)
	call("alpha", "star-message", target+"}", "absent", 400)
	call("alpha", "star-message", target+`,"star":false,"sender":"other"}`, "override", 400)
	call("beta", "star-message", off, "other", 404)
	call("alpha", "star-message", strings.Replace(off, "12345", "54321", 1), "wrong-chat", 404)
	call("alpha", "star-message", on, "first", 200)
	call("alpha", "star-message", off, "second", 200)
	call("alpha", "star-message", off, "second", 200)
	call("alpha", "star-message", on, "second", 409)
	if len(a.patches) != 2 {
		t.Fatal("unexpected protocol call count", len(a.patches))
	}
	for i, p := range a.patches {
		m := p.Mutations[0]
		if !slices.Equal(m.Index, []string{appstate.IndexStar, group.String(), "msg", "0", sender.String()}) || m.Value.GetStarAction().GetStarred() != (i == 0) {
			t.Fatal("wrong patch", m.Index)
		}
	}
	if !strings.Contains(call("alpha", "message-star-state", target+"}", "", 200), `"starred":null`) {
		t.Fatal("reported unobserved state")
	}
	call("alpha", "message-state-result", "", "second", 200)
	call("alpha", "send-result", "", "second", 404)
	call("beta", "message-state-result", "", "second", 404)
	a.failure = errors.New("not confirmed")
	for range 2 {
		call("alpha", "star-message", off, "uncertain", 502)
	}
	if len(a.patches) != 3 {
		t.Fatal("retransmitted after lost confirmation")
	}
}
