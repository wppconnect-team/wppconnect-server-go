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
	calls       int
	id, name    string
	ids         []string
	update      types.BusinessCollectionUpdate
	cartEnabled bool
	hidden      bool
	failure     error
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
func (f *shopWriter) SetBusinessCartEnabled(ctx context.Context, enabled bool) error {
	f.note(ctx)
	f.cartEnabled = enabled
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
		{cartEnabled, `{}`}, {cartEnabled, `{"enabled":null}`}, {cartEnabled, `{"enabled":"false"}`},
		{cartEnabled, `{"enabled":false,"seller":"other"}`},
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
	call("POST", cartEnabled, `{"enabled":true}`, "alpha", "beta", "forbidden-cart", 401)
	call("POST", cartEnabled, `{"enabled":true}`, "alpha", "alpha", "", 400)
	for _, enabled := range []bool{true, false} {
		body := `{"enabled":` + strconv.FormatBool(enabled) + `}`
		key := "cart-" + strconv.FormatBool(enabled)
		before := f.calls
		call("POST", cartEnabled, body, "alpha", "alpha", key, 201)
		replay := call("POST", cartEnabled, body, "alpha", "alpha", key, 201)
		if f.calls != before+1 || f.cartEnabled != enabled || replay.Header().Get("Idempotency-Replayed") != "true" || !strings.Contains(replay.Body.String(), body) {
			t.Fatalf("cart update or replay differed for enabled=%t", enabled)
		}
		call("GET", catalogWriteResult, "", "alpha", "alpha", key, 200)
		call("GET", catalogWriteResult, "", "beta", "beta", key, 404)
	}
	call("POST", cartEnabled, `{"enabled":true}`, "alpha", "alpha", "cart-false", 409)
	f.id = "wrong"
	call("POST", updateCollection, `{"collectionId":"collection","name":"Changed"}`, "alpha", "alpha", "wrong-result", 502)
	f.failure = errors.New("transport")
	before := f.calls
	for range 2 {
		call("POST", cartEnabled, `{"enabled":false}`, "alpha", "alpha", "cart-uncertain", 502)
	}
	if f.calls != before+1 {
		t.Fatal("unconfirmed cart setting retransmitted")
	}
	before = f.calls
	for range 2 {
		call("POST", productVisibility, `{"productId":"20","hidden":true}`, "alpha", "alpha", "uncertain", 502)
	}
	if f.calls != before+1 {
		t.Fatal("unconfirmed mutation retransmitted")
	}
}

type productShop struct {
	*shopWriter
	input      types.BusinessProductInput
	returnedID string
	deleted    int
}

func (f *productShop) DeleteBusinessCollections(ctx context.Context, ids []string) error {
	f.note(ctx)
	f.ids = ids
	return f.failure
}
func (f *productShop) CreateBusinessProduct(ctx context.Context, input types.BusinessProductInput, width, height int) (*types.BusinessProduct, error) {
	f.note(ctx)
	f.input = input
	return &types.BusinessProduct{ID: f.returnedID, Hidden: input.Hidden, Price: input.Price, Currency: input.Currency, Status: types.BusinessProductStatus{Status: "pending"}}, f.failure
}
func (f *productShop) UpdateBusinessProduct(ctx context.Context, id string, input types.BusinessProductInput, width, height int) (*types.BusinessProduct, error) {
	f.id = id
	return f.CreateBusinessProduct(ctx, input, width, height)
}
func (f *productShop) DeleteBusinessProducts(ctx context.Context, ids []string) (int, error) {
	f.note(ctx)
	f.ids = ids
	return f.deleted, f.failure
}
func TestOwnedCatalogProductContracts(t *testing.T) {
	h := newMediaHarness(t)
	f := &productShop{shopWriter: &shopWriter{roadmapChannels: &roadmapChannels{sender: h.client}, id: "collection", hidden: true}, returnedID: "p", deleted: 2}
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

	media := `"image_urls":["https://mmg.whatsapp.net/item"]`
	fields := `{"name":"Teste","price":"987654321098765432","currency":"BRL","is_hidden":true,` + media + `}`
	add := `{"product":` + fields + `}`
	edit := `{"productId":"p","product":` + strings.Replace(fields, "true", "false", 1) + `}`
	collections := `{"collectionIds":["c1","c2"]}`
	products := `{"productIds":["p1","p2"]}`
	ops := []struct {
		name, body string
		code       int
		fragment   string
	}{
		{deleteCollections, collections, 200, `"requestedCount":2`},
		{createProduct, add, 201, `"reviewStatus":"pending"`},
		{updateProduct, edit, 200, `"hidden":false`},
		{deleteProducts, products, 200, `"deletedCount":2`},
	}
	for _, op := range ops {
		call("POST", op.name, op.body, "alpha", "alpha", "", 400)
		call("POST", op.name, op.body, "alpha", "beta", "denied", 401)
		before := f.calls
		first := call("POST", op.name, op.body, "alpha", "alpha", op.name, op.code)
		replay := call("POST", op.name, op.body, "alpha", "alpha", op.name, op.code)
		if f.calls != before+1 || replay.Header().Get("Idempotency-Replayed") != "true" || first.Body.String() != replay.Body.String() || !strings.Contains(first.Body.String(), op.fragment) {
			t.Fatalf("%s replay or receipt: %s", op.name, first.Body.String())
		}
		switch op.name {
		case createProduct, updateProduct:
			if f.input.Price != "987654321098765432" || f.input.Currency != "BRL" || f.input.Hidden != (op.name == createProduct) || !strings.Contains(first.Body.String(), f.input.Price) || (op.name == updateProduct && f.id != "p") {
				t.Fatal("product identity, amount, currency or visibility changed")
			}
		case deleteCollections:
			if !reflect.DeepEqual(f.ids, []string{"c1", "c2"}) || strings.Contains(first.Body.String(), "deletedCount") {
				t.Fatal("invalid collection receipt", first.Body.String())
			}
		case deleteProducts:
			if !reflect.DeepEqual(f.ids, []string{"p1", "p2"}) {
				t.Fatal(f.ids)
			}
		}
		call("POST", op.name, op.body+" ", "alpha", "alpha", op.name, 409)
		call("GET", catalogWriteResult, "", "alpha", "alpha", op.name, 200)
		call("GET", catalogWriteResult, "", "beta", "beta", op.name, 404)
		call("GET", "newsletter-admin-result", "", "alpha", "alpha", op.name, 404)
	}
	before := f.calls
	for i, bad := range []struct{ name, body string }{
		{deleteCollections, `{"collectionIds":[]}`}, {deleteCollections, `{"collectionIds":["c","c"]}`},
		{deleteProducts, `{"productIds":["p","p"]}`}, {deleteProducts, `{"productIds":["p"],"seller":"other"}`},
		{createProduct, strings.Replace(add, "mmg.whatsapp.net", "example.test", 1)},
		{createProduct, strings.Replace(add, "987654321098765432", "12.34", 1)},
		{createProduct, strings.Replace(add, "BRL", "BR", 1)},
		{createProduct, `{"product":{"name":"item","image_urls":[]}}`},
		{createProduct, strings.Replace(add, `"name":`, `"seller":"other","name":`, 1)},
		{updateProduct, strings.Replace(edit, `"is_hidden":false,`, "", 1)},
		{updateProduct, strings.Replace(edit, `"productId":"p"`, `"productId":""`, 1)},
	} {
		call("POST", bad.name, bad.body, "alpha", "alpha", "bad-"+strconv.Itoa(i), 400)
	}
	if f.calls != before {
		t.Fatal("invalid request dispatched")
	}
	for _, count := range []int{0, 1, 3, -1} {
		f.deleted = count
		want := 409
		if count < 0 || count > 2 {
			want = 502
		}
		key := "count-" + strconv.Itoa(count)
		for range 2 {
			res := call("POST", deleteProducts, products, "alpha", "alpha", key, want)
			if want == 409 && (!strings.Contains(res.Body.String(), `"status":"partial"`) || !strings.Contains(res.Body.String(), `"deletedCount":`+strconv.Itoa(count))) {
				t.Fatal(res.Body.String())
			}
		}
	}
	if f.calls != before+4 {
		t.Fatal("repeated partial deletion")
	}
	f.returnedID = "wrong"
	call("POST", updateProduct, edit, "alpha", "alpha", "wrong-id", 502)
	f.returnedID = ""
	call("POST", createProduct, add, "alpha", "alpha", "empty-id", 502)
	f.failure = errors.New("fixture lost reply")
	for _, op := range ops {
		before := f.calls
		for range 2 {
			res := call("POST", op.name, op.body, "alpha", "alpha", "lost-"+op.name, 502)
			if !strings.Contains(res.Body.String(), "unconfirmed") {
				t.Fatal(res.Body.String())
			}
		}
		if f.calls != before+1 {
			t.Fatal("repeated uncertain command", op.name)
		}
	}
}
