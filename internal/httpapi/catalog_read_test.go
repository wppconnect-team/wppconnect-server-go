package httpapi

import (
	"context"
	"errors"
	"fmt"
	wm "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
	"github.com/wppconnect-team/wppconnect-server-go/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

type catalogFixture struct {
	*roadmapChannels
	calls       int
	owner       types.JID
	page        wm.GetCatalogParams
	collections wm.GetCollectionsParams
	mismatch    bool
	fail        bool
}

func (f *catalogFixture) check(ctx context.Context, j types.JID) {
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded catalog request")
	}
	f.calls++
	f.owner = j
}
func (f *catalogFixture) GetCatalog(ctx context.Context, j types.JID, p wm.GetCatalogParams) (*types.BusinessCatalogPage, error) {
	f.check(ctx, j)
	f.page = p
	if f.fail {
		return nil, errors.New("sensitive upstream error")
	}
	return &types.BusinessCatalogPage{Next: "opaque:next", Products: []types.BusinessProduct{{ID: "91", Price: "9007199254740993.0100", Currency: "BRL", Hidden: false, Compliance: &types.BusinessComplianceInfo{ImporterName: "private fixture"}}}}, nil
}
func (f *catalogFixture) GetCatalogProduct(ctx context.Context, j types.JID, id string) (*types.BusinessProduct, error) {
	f.check(ctx, j)
	if f.mismatch {
		id = "unexpected"
	}
	return &types.BusinessProduct{ID: id, Name: "Product", Hidden: true}, nil
}
func (f *catalogFixture) GetProductCollections(ctx context.Context, j types.JID, p wm.GetCollectionsParams) (*types.BusinessCollectionPage, error) {
	f.check(ctx, j)
	f.collections = p
	return &types.BusinessCollectionPage{Collections: []types.BusinessCollection{{ID: "8", Name: "Seasonal", Products: []types.BusinessProduct{{Name: "hidden nested fixture"}}}}}, nil
}
func TestOwnCatalogScopePagingAndPricePrecision(t *testing.T) {
	h := newMediaHarness(t)
	f := &catalogFixture{roadmapChannels: &roadmapChannels{sender: h.client}}
	api := (&Server{cfg: config.Config{SecretKey: "test-secret"}, channelSession: func(name string) (channels.Client, bool) { return f, name == "alpha" }}).router()
	post := func(op, body, token string, status int) string {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/alpha/"+op, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+h.tokens[token])
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", op, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	post("own-catalog", "{}", "beta", 401)
	post("own-catalog", `{"businessJID":"123@s.whatsapp.net"}`, "alpha", 400)
	post("own-catalog", `{"limit":0}`, "alpha", 400)
	post("own-catalog-collections", `{"limit":21}`, "alpha", 400)
	post("own-catalog-product", `{"productId":""}`, "alpha", 400)
	if f.calls != 0 {
		t.Fatal("invalid or cross-session calls reached SDK")
	}
	v := post("own-catalog", `{"after":"opaque:1","limit":1}`, "alpha", 200)
	if f.owner != f.Self() || f.page.After != "opaque:1" || f.page.Limit != 1 || !strings.Contains(v, `"price":"9007199254740993.0100"`) || !strings.Contains(v, `"currency":"BRL"`) || !strings.Contains(v, `"hidden":false`) || strings.Contains(v, "private fixture") {
		t.Fatal("invalid projection or account scope", v)
	}
	v = post("own-catalog-collections", `{}`, "alpha", 200)
	if f.collections.ItemLimit != 1 || f.collections.CollectionLimit != 20 || !strings.Contains(v, "Seasonal") || strings.Contains(v, "hidden nested fixture") {
		t.Fatal("incorrect collection summary", v)
	}
	post("own-catalog-product", `{"productId":"99"}`, "alpha", 200)
	f.mismatch = true
	post("own-catalog-product", `{"productId":"99"}`, "alpha", 502)
	f.fail = true
	if strings.Contains(post("own-catalog", `{}`, "alpha", 502), "sensitive upstream") {
		t.Fatal("exposed upstream details")
	}
}

func TestCatalogReadFailureIdentifiesSourceWithoutExposingMerchant(t *testing.T) {
	cases := []struct {
		failure error
		status  int
		message string
	}{
		{fmt.Errorf("QueryCatalog: %w", types.GraphQLErrors{{Message: "private merchant fixture", Path: []string{"secret-path"}, Extensions: types.GraphQLErrorExtensions{ErrorCode: 400}}}), 502, "WhatsApp catalog query failed (GraphQL 400)"},
		{fmt.Errorf("QueryProductCollections: %w", context.DeadlineExceeded), 504, "WhatsApp catalog query failed"},
		{wm.ErrNotConnected, 503, "WhatsApp catalog query failed"},
		{errors.New("private merchant fixture"), 502, "WhatsApp catalog query failed"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		catalogReadError(w, tc.failure)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.message) || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "secret-path") {
			t.Errorf("unexpected catalog response: %d %s", w.Code, w.Body)
		}
	}
}
