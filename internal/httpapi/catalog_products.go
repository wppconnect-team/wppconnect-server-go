package httpapi

import (
	"context"
	"errors"
	"net/http"

	wm "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/types"
	"github.com/wppconnect-team/wppconnect-server-go/internal/channels"
)

const (
	deleteCollections = "del-collection"
	createProduct     = "add-product"
	updateProduct     = "edit-product"
	deleteProducts    = "del-products"
)

type catalogProductWriter interface {
	DeleteBusinessCollections(context.Context, []string) error
	CreateBusinessProduct(context.Context, types.BusinessProductInput, int, int) (*types.BusinessProduct, error)
	UpdateBusinessProduct(context.Context, string, types.BusinessProductInput, int, int) (*types.BusinessProduct, error)
	DeleteBusinessProducts(context.Context, []string) (int, error)
}

var _ catalogProductWriter = channels.SDK{}

type collectionBatch struct {
	IDs []string `json:"collectionIds"`
}
type productBatch struct {
	IDs []string `json:"productIds"`
}

// The explicit visibility field distinguishes false from an omitted edit value.
type productFields struct {
	types.BusinessProductInput
	Visibility *bool `json:"is_hidden"`
}
type productCreateRequest struct {
	Product productFields `json:"product"`
}
type productEditRequest struct {
	ProductID string        `json:"productId"`
	Product   productFields `json:"product"`
}

func validBatch(w http.ResponseWriter, ids []string) bool {
	if len(ids) > 0 && validateCollectionIDs(ids, nil) {
		return true
	}
	invalidCatalog(w, errors.New("provide 1..100 distinct IDs"))
	return false
}

func writeProducts(ctx context.Context, w http.ResponseWriter, r *http.Request, name string, api catalogProductWriter) {
	switch name {
	case deleteCollections:
		var q collectionBatch
		if err := readStructured(w, r, &q); err != nil {
			invalidCatalog(w, err)
			return
		}
		if !validBatch(w, q.IDs) {
			return
		}
		if err := api.DeleteBusinessCollections(ctx, q.IDs); err != nil {
			catalogUpstreamError(w)
			return
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": map[string]any{"acknowledged": true, "requestedCount": len(q.IDs)}})
	case deleteProducts:
		var q productBatch
		if err := readStructured(w, r, &q); err != nil {
			invalidCatalog(w, err)
			return
		}
		if !validBatch(w, q.IDs) {
			return
		}
		count, err := api.DeleteBusinessProducts(ctx, q.IDs)
		if err != nil || count < 0 || count > len(q.IDs) {
			catalogUpstreamError(w)
			return
		}
		counts := map[string]int{"requestedCount": len(q.IDs), "deletedCount": count}
		if count != len(q.IDs) {
			writeJSON(w, 409, map[string]any{"status": "partial", "response": counts, "message": "read each product before issuing a new operation"})
			return
		}
		writeJSON(w, 200, map[string]any{"status": "success", "response": counts})
	case createProduct, updateProduct:
		writeSingleProduct(ctx, w, r, name, api)
	}
}

func writeSingleProduct(ctx context.Context, w http.ResponseWriter, r *http.Request, name string, api catalogProductWriter) {
	var id string
	var fields productFields
	if name == updateProduct {
		var q productEditRequest
		if err := readStructured(w, r, &q); err != nil {
			invalidCatalog(w, err)
			return
		}
		if !catalogValue(q.ProductID) || q.Product.Visibility == nil {
			invalidCatalog(w, errors.New("editing requires productId and explicit product.is_hidden"))
			return
		}
		id, fields = q.ProductID, q.Product
	} else {
		var q productCreateRequest
		if err := readStructured(w, r, &q); err != nil {
			invalidCatalog(w, err)
			return
		}
		fields = q.Product
	}
	input := fields.BusinessProductInput
	if fields.Visibility != nil {
		input.Hidden = *fields.Visibility
	}
	if err := wm.ValidateBusinessProductInput(input); err != nil {
		invalidCatalog(w, err)
		return
	}
	var product *types.BusinessProduct
	var err error
	code := http.StatusCreated
	if name == updateProduct {
		product, err = api.UpdateBusinessProduct(ctx, id, input, 100, 100)
		code = http.StatusOK
	} else {
		product, err = api.CreateBusinessProduct(ctx, input, 100, 100)
	}
	if err != nil || product == nil || !catalogValue(product.ID) || (id != "" && product.ID != id) {
		catalogUpstreamError(w)
		return
	}
	writeJSON(w, code, map[string]any{"status": "success", "response": map[string]any{"product": catalogView(*product), "reviewStatus": product.Status.Status}})
}

func productWriteContract(name string, c routeContract) routeContract {
	batch := schema{"type": "array", "minItems": 1, "maxItems": 100, "uniqueItems": true, "items": schema{"type": "string", "minLength": 1, "maxLength": 256}}
	switch name {
	case deleteCollections:
		c.body = object(schema{"collectionIds": batch}, "collectionIds")
		c.response = envelope(object(schema{"acknowledged": boolean(), "requestedCount": integer()}, "acknowledged", "requestedCount"))
		c.description += " Remote success acknowledges the batch. Read own-catalog-collections to verify individual IDs."
	case deleteProducts:
		c.body = object(schema{"productIds": batch}, "productIds")
		c.response = envelope(object(schema{"deletedCount": integer(), "requestedCount": integer()}, "deletedCount", "requestedCount"))
		c.description += " A partial count returns HTTP 409 and both counts. Check each product with own-catalog-product before another operation."
	case createProduct, updateProduct:
		fields := requestSchema(types.BusinessProductInput{}, "name", "image_urls")
		fields["properties"].(schema)["image_urls"] = schema{"type": "array", "minItems": 1, "maxItems": 10, "items": str()}
		if name == createProduct {
			c.body = object(schema{"product": fields}, "product")
			c.success = 201
		} else {
			fields["required"] = []string{"name", "image_urls", "is_hidden"}
			c.body = object(schema{"product": fields, "productId": str()}, "product", "productId")
		}
		c.response = envelope(object(schema{"product": requestSchema(catalogProductView{}), "reviewStatus": str()}, "product", "reviewStatus"))
		c.description += " Supply the full product on edit including is_hidden. Price and sale_price are integer strings in thousandths, with a three-letter currency. Supply 1..10 existing HTTPS media references on WhatsApp or Meta hosts."
	}
	c.body["additionalProperties"] = false
	return c
}
