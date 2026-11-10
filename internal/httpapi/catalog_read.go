package httpapi

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	wm "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// Catalog sessions can only inspect their own connected business. The HTTP
// contract never accepts another seller's identity, even as a query override.
type catalogReader interface {
	GetCatalog(context.Context, types.JID, wm.GetCatalogParams) (*types.BusinessCatalogPage, error)
	GetCatalogProduct(context.Context, types.JID, string) (*types.BusinessProduct, error)
	GetProductCollections(context.Context, types.JID, wm.GetCollectionsParams) (*types.BusinessCollectionPage, error)
}

var _ catalogReader = channels.SDK{}

type catalogPageRequest struct {
	Limit *int   `json:"limit"`
	After string `json:"after"`
}
type catalogProductRequest struct {
	ProductID string `json:"productId"`
}
type catalogProductView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Price        string `json:"price"`
	Currency     string `json:"currency"`
	Hidden       bool   `json:"hidden"`
	Availability string `json:"availability"`
}

func catalogView(p types.BusinessProduct) catalogProductView {
	return catalogProductView{p.ID, p.Name, p.Price, p.Currency, p.Hidden, p.Availability}
}
func (s *Server) catalogRoutes(r chi.Router) {
	for _, op := range []string{createCollection, updateCollection, productVisibility} {
		r.Post("/api/{session}/"+op, s.writeCatalog(op))
	}
	for _, op := range []string{"own-catalog", "own-catalog-product", "own-catalog-collections"} {
		r.Post("/api/{session}/"+op, s.readCatalog(op))
	}
}
func (s *Server) readCatalog(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.channelConnection(w, r)
		if !ok {
			return
		}
		reader, ok := c.(catalogReader)
		if !ok {
			writeJSON(w, 501, map[string]any{"status": "not_supported"})
			return
		}
		owner := c.Self().ToNonAD()
		if owner.User == "" || owner.Server != types.DefaultUserServer {
			writeJSON(w, 409, map[string]any{"status": "error", "message": "connected account phone identity unavailable"})
			return
		}
		if !s.acquireMedia(w, r) {
			return
		}
		defer func() { <-s.mediaSlots }()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var out any
		if op == "own-catalog-product" {
			var q catalogProductRequest
			if err := readStructured(w, r, &q); err != nil {
				mediaError(w, err, "")
				return
			}
			if strings.TrimSpace(q.ProductID) == "" || len(q.ProductID) > 256 || strings.ContainsFunc(q.ProductID, unicode.IsControl) {
				mediaError(w, errors.New("productId must be a nonempty identifier of at most 256 bytes"), "")
				return
			}
			p, err := reader.GetCatalogProduct(ctx, owner, q.ProductID)
			if err != nil {
				directoryError(w, err)
				return
			}
			if p == nil {
				writeJSON(w, 404, map[string]any{"status": "not_found"})
				return
			}
			if p.ID != q.ProductID {
				catalogMismatch(w)
				return
			}
			out = catalogView(*p)
		} else {
			var q catalogPageRequest
			if err := readStructured(w, r, &q); err != nil {
				mediaError(w, err, "")
				return
			}
			limit := 20
			if q.Limit != nil {
				limit = *q.Limit
			}
			maxLimit := 50
			if op == "own-catalog-collections" {
				maxLimit = 20
			}
			if limit < 1 || limit > maxLimit || len(q.After) > 2048 || strings.ContainsFunc(q.After, unicode.IsControl) {
				mediaError(w, errors.New("product limit is 1..50, collection limit is 1..20 and after is at most 2048 bytes"), "")
				return
			}
			if op == "own-catalog" {
				p, err := reader.GetCatalog(ctx, owner, wm.GetCatalogParams{After: q.After, Limit: limit})
				if err != nil {
					directoryError(w, err)
					return
				}
				if p == nil || len(p.Products) > limit || len(p.Next) > 2048 {
					catalogMismatch(w)
					return
				}
				rows := make([]catalogProductView, 0, len(p.Products))
				for _, v := range p.Products {
					if v.ID == "" {
						catalogMismatch(w)
						return
					}
					rows = append(rows, catalogView(v))
				}
				out = map[string]any{"products": rows, "next": p.Next}
			} else {
				p, err := reader.GetProductCollections(ctx, owner, wm.GetCollectionsParams{After: q.After, CollectionLimit: limit, ItemLimit: 1})
				if err != nil {
					directoryError(w, err)
					return
				}
				if p == nil || len(p.Collections) > limit || len(p.Next) > 2048 {
					catalogMismatch(w)
					return
				}
				rows := make([]map[string]string, 0, len(p.Collections))
				for _, v := range p.Collections {
					if v.ID == "" {
						catalogMismatch(w)
						return
					}
					rows = append(rows, map[string]string{"id": v.ID, "name": v.Name})
				}
				out = map[string]any{"collections": rows, "next": p.Next}
			}
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": out})
	}
}
func catalogMismatch(w http.ResponseWriter) {
	writeJSON(w, 502, map[string]any{"status": "error", "message": "unexpected catalog response from upstream"})
}
func catalogContract(name string) (routeContract, bool) {
	if catalogMutation(name) {
		return catalogWriteContract(name)
	}
	c := routeContract{tag: "business", success: 200}
	product := requestSchema(catalogProductView{}, "id", "name", "price", "currency", "hidden", "availability")
	switch name {
	case "own-catalog", "own-catalog-collections":
		c.body = requestSchema(catalogPageRequest{})
		c.body["properties"].(schema)["limit"] = schema{"type": "integer", "minimum": 1, "maximum": 50, "default": 20}
		if name == "own-catalog-collections" {
			c.body["properties"].(schema)["limit"].(schema)["maximum"] = 20
		}
		c.description = "Read the connected account's own catalog. Opaque after cursor up to 2048 bytes; product limit 1..50, collection limit 1..20 (default 20). Collections return summaries only. No seller override, files, images or mutations."
		if name == "own-catalog" {
			c.response = envelope(object(schema{"products": array(product), "next": str()}, "products", "next"))
		} else {
			c.response = envelope(object(schema{"collections": array(object(schema{"id": str(), "name": str()}, "id", "name")), "next": str()}, "collections", "next"))
		}
	case "own-catalog-product":
		c.body = requestSchema(catalogProductRequest{}, "productId")
		c.response = envelope(product)
		c.description = "Read one product from the connected account's catalog. Product ID is an opaque identifier of up to 256 bytes. Price and currency are upstream strings, without numeric conversion."
	default:
		return routeContract{}, false
	}
	return c, true
}
