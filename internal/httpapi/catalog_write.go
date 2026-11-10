package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
)

type catalogWriter interface {
	CreateBusinessCollection(context.Context, string, []string) (*types.BusinessCollectionMutationResult, error)
	UpdateBusinessCollection(context.Context, string, types.BusinessCollectionUpdate) (*types.BusinessCollectionMutationResult, error)
	SetBusinessProductVisibility(context.Context, string, bool) error
}

var _ catalogWriter = channels.SDK{}

const createCollection = "create-own-collection"
const updateCollection = "update-own-collection"
const productVisibility = "set-own-product-visibility"
const catalogWriteResult = "catalog-write-result"

func catalogMutation(name string) bool {
	return name == createCollection || name == updateCollection || name == productVisibility
}

type newCollection struct {
	Name       string   `json:"name"`
	ProductIDs []string `json:"productIds"`
}
type collectionChange struct {
	CollectionID string   `json:"collectionId"`
	Name         *string  `json:"name,omitempty"`
	Add          []string `json:"addProductIds,omitempty"`
	Remove       []string `json:"removeProductIds,omitempty"`
}
type visibilityChange struct {
	ProductID string `json:"productId"`
	Hidden    *bool  `json:"hidden"`
}

func catalogValue(s string) bool {
	return s != "" && s == strings.TrimSpace(s) && len(s) <= 256 && !strings.ContainsFunc(s, unicode.IsControl)
}
func validateCollectionIDs(add, remove []string) bool {
	if len(add)+len(remove) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, group := range [][]string{add, remove} {
		for _, id := range group {
			if !catalogValue(id) || seen[id] {
				return false
			}
			seen[id] = true
		}
	}
	return true
}
func invalidCatalog(w http.ResponseWriter, e error) { mediaError(w, e, "") }
func catalogUpstreamError(w http.ResponseWriter) {
	writeJSON(w, 502, map[string]any{"status": "unconfirmed", "message": "catalog mutation was not confirmed; consult the result and read the catalog before issuing a new operation"})
}
func renderCollection(w http.ResponseWriter, v *types.BusinessCollectionMutationResult, expected string, err error, code int) {
	if err != nil || v == nil || !catalogValue(v.ID) || (expected != "" && v.ID != expected) {
		catalogUpstreamError(w)
		return
	}
	writeJSON(w, code, map[string]any{"status": "success", "response": map[string]string{"id": v.ID, "reviewStatus": v.ReviewStatus}})
}
func (s *Server) writeCatalog(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") == "" {
			commandError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "a catalog mutation requires Idempotency-Key")
			return
		}
		if r.URL.RawQuery != "" {
			invalidCatalog(w, errors.New("unexpected catalog query parameters"))
			return
		}
		c, ok := s.channelConnection(w, r)
		if !ok {
			return
		}
		owner := c.Self().ToNonAD()
		if owner.User == "" || owner.Server != types.DefaultUserServer {
			writeJSON(w, 409, map[string]any{"status": "error", "message": "connected account phone identity unavailable"})
			return
		}
		api, ok := c.(catalogWriter)
		if !ok {
			writeJSON(w, 501, map[string]any{"status": "not_supported"})
			return
		}
		if !s.acquireMedia(w, r) {
			return
		}
		defer func() { <-s.mediaSlots }()
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		switch name {
		case createCollection:
			var q newCollection
			if e := readStructured(w, r, &q); e != nil {
				invalidCatalog(w, e)
				return
			}
			if !catalogValue(q.Name) || len(q.ProductIDs) == 0 || !validateCollectionIDs(q.ProductIDs, nil) {
				invalidCatalog(w, errors.New("provide a name and 1..100 distinct product IDs"))
				return
			}
			v, e := api.CreateBusinessCollection(ctx, q.Name, q.ProductIDs)
			renderCollection(w, v, "", e, 201)
		case updateCollection:
			var q collectionChange
			if e := readStructured(w, r, &q); e != nil {
				invalidCatalog(w, e)
				return
			}
			if !catalogValue(q.CollectionID) || (q.Name != nil && !catalogValue(*q.Name)) || (q.Name == nil && len(q.Add)+len(q.Remove) == 0) || !validateCollectionIDs(q.Add, q.Remove) {
				invalidCatalog(w, errors.New("provide a collectionId and a name or up to 100 disjoint product changes"))
				return
			}
			v, e := api.UpdateBusinessCollection(ctx, q.CollectionID, types.BusinessCollectionUpdate{Name: q.Name, AddProductIDs: q.Add, RemoveProductIDs: q.Remove})
			renderCollection(w, v, q.CollectionID, e, 200)
		case productVisibility:
			var q visibilityChange
			if e := readStructured(w, r, &q); e != nil {
				invalidCatalog(w, e)
				return
			}
			if !catalogValue(q.ProductID) || q.Hidden == nil {
				invalidCatalog(w, errors.New("provide a productId and explicit hidden boolean"))
				return
			}
			if e := api.SetBusinessProductVisibility(ctx, q.ProductID, *q.Hidden); e != nil {
				catalogUpstreamError(w)
				return
			}
			writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"productId": q.ProductID, "hidden": *q.Hidden}})
		}
	}
}
func catalogWriteContract(name string) (routeContract, bool) {
	c := routeContract{tag: "business", success: 200, description: "Mutate only the connected account's own catalog. Idempotency-Key is required. Recover the original result via catalog-write-result. Remote review can remain pending."}
	collection := envelope(object(schema{"id": str(), "reviewStatus": str()}, "id", "reviewStatus"))
	switch name {
	case createCollection:
		c.success = 201
		c.body = requestSchema(newCollection{}, "name", "productIds")
		c.response = collection
	case updateCollection:
		c.body = requestSchema(collectionChange{}, "collectionId")
		c.response = collection
	case productVisibility:
		c.body = requestSchema(visibilityChange{}, "productId", "hidden")
		c.response = envelope(object(schema{"productId": str(), "hidden": schema{"type": "boolean"}}, "productId", "hidden"))
	default:
		return c, false
	}
	return c, true
}
