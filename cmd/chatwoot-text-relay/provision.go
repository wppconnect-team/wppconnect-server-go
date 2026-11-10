package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

type routeSetup struct {
	db                  *sql.DB
	client              *http.Client
	root, token, marker string
	route               binding
}
type contactRecord struct {
	ID      int64
	Phone   string `json:"phone_number"`
	Blocked bool
	Links   []struct {
		Source string `json:"source_id"`
		Inbox  struct{ ID int64 }
	} `json:"contact_inboxes"`
}
type conversationRecord struct {
	ID      int64
	Account int64 `json:"account_id"`
	Inbox   int64 `json:"inbox_id"`
	Meta    struct {
		Channel string
		Sender  struct{ ID int64 }
	}
	Attributes struct {
		Route string `json:"wppconnect_route"`
	} `json:"additional_attributes"`
}

func resolveRoute(ctx context.Context, dir, origin, apiToken string, client *http.Client, b binding) (binding, error) {
	setupPath := filepath.Join(dir, "provision.db")
	if _, err := os.Stat(setupPath); errors.Is(err, os.ErrNotExist) {
		if _, err = os.Stat(filepath.Join(dir, "receipts.db")); !errors.Is(err, os.ErrNotExist) {
			return b, errors.New("existing manual receipts require the original contact and conversation IDs")
		}
	}
	f, err := os.OpenFile(setupPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return b, err
	}
	if err = f.Close(); err != nil {
		return b, err
	}
	db, err := sql.Open("sqlite", setupPath)
	if err != nil {
		return b, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
 CREATE TABLE IF NOT EXISTS provision_route(id INTEGER PRIMARY KEY CHECK(id=1),marker TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS provision_steps(name TEXT PRIMARY KEY,remote_id INTEGER NOT NULL DEFAULT 0);`)
	if err != nil {
		return b, err
	}
	// The token can rotate. The account, inbox, session, peer and origin cannot.
	identity, _ := json.Marshal([]any{origin, b.Account, b.Inbox, b.Session, b.Phone})
	marker := fmt.Sprintf("wpp-%x", sha256.Sum256(identity))
	if _, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO provision_route VALUES(1,?)", marker); err != nil {
		return b, err
	}
	var saved string
	if err = db.QueryRowContext(ctx, "SELECT marker FROM provision_route WHERE id=1").Scan(&saved); err != nil {
		return b, err
	}
	if saved != marker {
		return b, errors.New("provisioning database belongs to another route")
	}
	setup := routeSetup{db, client, fmt.Sprintf("%s/api/v1/accounts/%d", origin, b.Account), apiToken, marker, b}
	return setup.resolve(ctx)
}
func (p *routeSetup) api(ctx context.Context, method, path string, body any, dest any) bool {
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			return false
		}
	}
	return chatwootRequest(ctx, p.client, p.token, method, p.root+path, raw, dest)
}
func (p *routeSetup) known(ctx context.Context, name string) (int64, error) {
	var id int64
	err := p.db.QueryRowContext(ctx, "SELECT remote_id FROM provision_steps WHERE name=?", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// An outstanding reservation can only be reconciled by reading Chatwoot.
// A lost response or a competing relay never grants another create request.
func (p *routeSetup) reserve(ctx context.Context, name string) (bool, error) {
	res, err := p.db.ExecContext(ctx, "INSERT OR IGNORE INTO provision_steps(name) VALUES(?)", name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
func (p *routeSetup) record(ctx context.Context, name string, id int64) error {
	res, err := p.db.ExecContext(ctx, `INSERT INTO provision_steps(name,remote_id) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET remote_id=excluded.remote_id WHERE remote_id IN(0,excluded.remote_id)`, name, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("Chatwoot resource identity changed")
	}
	return err
}
func (p *routeSetup) lookupContact(ctx context.Context) (contactRecord, error) {
	var match contactRecord
	phone := "+" + p.route.Phone
	for page := 1; page <= 10; page++ {
		var list struct {
			Payload *[]contactRecord
			Meta    struct {
				More *bool `json:"has_more"`
			}
		}
		path := fmt.Sprintf("/contacts/search?q=%s&include_contact_inboxes=true&page=%d", url.QueryEscape(phone), page)
		if !p.api(ctx, "GET", path, nil, &list) || list.Payload == nil || list.Meta.More == nil {
			return match, errors.New("contact search unavailable")
		}
		for _, c := range *list.Payload {
			if c.Phone != phone {
				continue
			}
			if c.Blocked || c.ID <= 0 || (match.ID != 0 && match.ID != c.ID) {
				return match, errors.New("contact blocked or ambiguous")
			}
			match = c
		}
		if !*list.Meta.More {
			return match, nil
		}
	}
	return match, errors.New("contact search exceeded page limit")
}
func (p *routeSetup) contact(ctx context.Context) (contactRecord, error) {
	var c contactRecord
	saved, err := p.known(ctx, "contact")
	if err != nil {
		return c, err
	}
	if saved > 0 {
		path := fmt.Sprintf("/contacts/%d?include_contact_inboxes=true", saved)
		var show struct{ Payload contactRecord }
		if !p.api(ctx, "GET", path, nil, &show) || show.Payload.ID != saved || show.Payload.Phone != "+"+p.route.Phone || show.Payload.Blocked {
			return c, errors.New("stored Chatwoot contact has changed or is unavailable")
		}
		return show.Payload, nil
	}
	c, err = p.lookupContact(ctx)
	if err != nil {
		return c, err
	}
	if c.ID == 0 {
		claim, err := p.reserve(ctx, "contact")
		if err != nil {
			return c, err
		}
		if claim {
			var out json.RawMessage
			p.api(ctx, "POST", "/contacts", map[string]any{"name": "WhatsApp +" + p.route.Phone, "phone_number": "+" + p.route.Phone, "identifier": p.marker, "inbox_id": p.route.Inbox, "source_id": p.marker}, &out)
		}
		c, err = p.lookupContact(ctx)
		if err != nil {
			return c, err
		}
		if c.ID == 0 {
			return c, errors.New("contact creation awaits reconciliation")
		}
	}
	return c, p.record(ctx, "contact", c.ID)
}
func (p *routeSetup) inboxSource(c contactRecord) (string, error) {
	var source string
	for _, l := range c.Links {
		if l.Inbox.ID == p.route.Inbox {
			if l.Source == "" || source != "" {
				return "", errors.New("ambiguous Chatwoot inbox link")
			}
			source = l.Source
		}
	}
	return source, nil
}
func (p *routeSetup) link(ctx context.Context, c contactRecord) (string, error) {
	source, err := p.inboxSource(c)
	if err != nil || source != "" {
		return source, err
	}
	claim, err := p.reserve(ctx, "link")
	if err != nil {
		return "", err
	}
	if claim {
		var out json.RawMessage
		p.api(ctx, "POST", fmt.Sprintf("/contacts/%d/contact_inboxes", c.ID), map[string]any{"inbox_id": p.route.Inbox, "source_id": p.marker}, &out)
	}
	c, err = p.contact(ctx)
	if err != nil {
		return "", err
	}
	source, err = p.inboxSource(c)
	if err == nil && source == "" {
		err = errors.New("inbox link awaits reconciliation")
	}
	return source, err
}
func (p *routeSetup) matches(c conversationRecord) bool {
	return c.ID > 0 && c.Account == p.route.Account && c.Inbox == p.route.Inbox && c.Meta.Channel == "Channel::Api" && c.Meta.Sender.ID == p.route.Contact
}
func (p *routeSetup) verifyConversation(ctx context.Context, id int64) bool {
	var c conversationRecord
	return p.api(ctx, "GET", fmt.Sprintf("/conversations/%d", id), nil, &c) && c.ID == id && p.matches(c) && c.Attributes.Route == p.marker
}
func (p *routeSetup) conversation(ctx context.Context, source string) (int64, error) {
	saved, err := p.known(ctx, "conversation")
	if err != nil {
		return 0, err
	}
	if saved > 0 {
		if !p.verifyConversation(ctx, saved) {
			return 0, errors.New("stored conversation no longer matches route")
		}
		return saved, nil
	}
	find := func() (int64, error) {
		var list struct{ Payload *[]conversationRecord }
		if !p.api(ctx, "GET", fmt.Sprintf("/contacts/%d/conversations", p.route.Contact), nil, &list) || list.Payload == nil {
			return 0, errors.New("conversation lookup unavailable")
		}
		var id int64
		for _, c := range *list.Payload {
			if c.Attributes.Route != p.marker {
				continue
			}
			if !p.matches(c) || id != 0 {
				return 0, errors.New("ambiguous conversation marker")
			}
			id = c.ID
		}
		if id == 0 && len(*list.Payload) >= 25 {
			return 0, errors.New("conversation history exceeds lookup window")
		}
		return id, nil
	}
	id, err := find()
	if err != nil {
		return 0, err
	}
	if id == 0 {
		claim, err := p.reserve(ctx, "conversation")
		if err != nil {
			return 0, err
		}
		if claim {
			var out conversationRecord
			if p.api(ctx, "POST", "/conversations", map[string]any{"inbox_id": p.route.Inbox, "contact_id": p.route.Contact, "source_id": source, "additional_attributes": map[string]string{"wppconnect_route": p.marker}}, &out) && out.ID > 0 && p.verifyConversation(ctx, out.ID) {
				id = out.ID
			}
		}
		if id == 0 {
			id, err = find()
			if err != nil {
				return 0, err
			}
		}
	}
	if id == 0 || !p.verifyConversation(ctx, id) {
		return 0, errors.New("conversation creation awaits reconciliation")
	}
	return id, p.record(ctx, "conversation", id)
}
func (p *routeSetup) resolve(ctx context.Context) (binding, error) {
	var inbox struct {
		ID   int64
		Type string `json:"channel_type"`
	}
	if !p.api(ctx, "GET", fmt.Sprintf("/inboxes/%d", p.route.Inbox), nil, &inbox) || inbox.ID != p.route.Inbox || inbox.Type != "Channel::Api" {
		return p.route, errors.New("authorized API inbox required")
	}
	c, err := p.contact(ctx)
	if err != nil {
		return p.route, err
	}
	source, err := p.link(ctx, c)
	if err != nil {
		return p.route, err
	}
	p.route.Contact = c.ID
	p.route.Conversation, err = p.conversation(ctx, source)
	return p.route, err
}
