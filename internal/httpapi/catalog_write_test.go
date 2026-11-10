package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"github.com/wppconnect-team/wppconnect-server-go/internal/sendledger"
)

type shopWriter struct {
	*roadmapChannels
	calls    int
	id, name string
	ids      []string
	update   types.BusinessCollectionUpdate
	hidden   bool
	failure  error
}

func (f *shopWriter) note(ctx context.Context) {
	f.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded mutation")
	}
}
func (f *shopWriter) CreateBusinessCollection(ctx context.Context, name string, ids []string) (*types.BusinessCollectionMutationResult, error) {
	f.note(ctx)
	f.name = name
	f.ids = ids
	return &types.BusinessCollectionMutationResult{ID: "collection", ReviewStatus: "pending"}, f.failure
}
func (f *shopWriter) UpdateBusinessCollection(ctx context.Context, id string, update types.BusinessCollectionUpdate) (*types.BusinessCollectionMutationResult, error) {
	f.note(ctx)
	f.update = update
	return &types.BusinessCollectionMutationResult{ID: f.id}, f.failure
}
func (f *shopWriter) SetBusinessProductVisibility(ctx context.Context, id string, hidden bool) error {
	f.note(ctx)
	f.id = id
	f.hidden = hidden
	return f.failure
}
func TestOwnCatalogMutationContracts(t *testing.T) {
	h := newMediaHarness(t)
	f := &shopWriter{roadmapChannels: &roadmapChannels{sender: h.client}, id: "collection", hidden: true}
	ledger, err := sendledger.Open(filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	api := (&Server{cfg: config.Config{SecretKey: "test-secret"}, sends: ledger, channelSession: func(name string) (channels.Client, bool) { return f, name == "alpha" }}).router()
	call := func(method, op, body, session, token, key string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/"+session+"/"+op, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+h.tokens[token])
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: HTTP %d %s", method, op, w.Code, w.Body.String())
		}
		return w
	}
	body := `{"name":"Summer","productIds":["10","20"]}`
	call("POST", createCollection, body, "alpha", "alpha", "", 400)
	call("POST", createCollection, body, "alpha", "beta", "forbidden", 401)
	for i, v := range []struct{ op, body string }{
		{createCollection, `{"name":"sale","productIds":["a","a"]}`}, {createCollection, `{"name":"sale","productIds":[]}`},
		{updateCollection, `{"collectionId":"c"}`}, {updateCollection, `{"collectionId":"c","addProductIds":["x"],"removeProductIds":["x"]}`},
		{productVisibility, `{"productId":"p"}`}, {productVisibility, `{"productId":"p","hidden":false,"seller":"other"}`},
	} {
		call("POST", v.op, v.body, "alpha", "alpha", "invalid-"+strconv.Itoa(i), 400)
	}
	if f.calls != 0 {
		t.Fatal("invalid mutation reached protocol")
	}
	first := call("POST", createCollection, body, "alpha", "alpha", "create-key", 201)
	again := call("POST", createCollection, body, "alpha", "alpha", "create-key", 201)
	if f.calls != 1 || f.name != "Summer" || !reflect.DeepEqual(f.ids, []string{"10", "20"}) || first.Body.String() != again.Body.String() || again.Header().Get("Idempotency-Replayed") != "true" || !strings.Contains(first.Body.String(), `"reviewStatus":"pending"`) {
		t.Fatal("invalid create response/replay")
	}
	call("POST", updateCollection, `{"collectionId":"collection","name":"Changed","addProductIds":["30"],"removeProductIds":["10"]}`, "alpha", "alpha", "edit-key", 200)
	if f.update.Name == nil || *f.update.Name != "Changed" || !reflect.DeepEqual(f.update.AddProductIDs, []string{"30"}) || !reflect.DeepEqual(f.update.RemoveProductIDs, []string{"10"}) {
		t.Fatal("invalid protocol changes")
	}
	if w := call("POST", productVisibility, `{"productId":"20","hidden":false}`, "alpha", "alpha", "visibility-key", 200); f.hidden || f.id != "20" || !strings.Contains(w.Body.String(), `"hidden":false`) {
		t.Fatal("false became true")
	}
	call("POST", productVisibility, `{"productId":"20","hidden":true}`, "alpha", "alpha", "visibility-key", 409)
	call("GET", catalogWriteResult, "", "alpha", "alpha", "create-key", 200)
	for _, op := range []string{"send-result", "newsletter-write-result", "newsletter-admin-result"} {
		call("GET", op, "", "alpha", "alpha", "create-key", 404)
	}
	call("GET", catalogWriteResult, "", "beta", "beta", "create-key", 404)
	f.id = "wrong"
	call("POST", updateCollection, `{"collectionId":"collection","name":"Changed"}`, "alpha", "alpha", "wrong-result", 502)
	f.failure = errors.New("transport")
	before := f.calls
	for range 2 {
		call("POST", productVisibility, `{"productId":"20","hidden":true}`, "alpha", "alpha", "uncertain", 502)
	}
	if f.calls != before+1 {
		t.Fatal("unconfirmed mutation retransmitted")
	}
}
