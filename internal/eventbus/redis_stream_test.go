package eventbus

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestRedisStreamConfiguration(t *testing.T) {
	for _, raw := range []string{"http://other", "redis://", "redis://:private@host/x", "rediss://:private@host/0?skip_verify=true"} {
		if _, err := NewRedisStream(raw, "events", 20); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid configuration accepted or credentials exposed")
		}
	}
	if _, err := NewRedisStream("redis://localhost", "unsafe key", 20); err == nil {
		t.Fatal("invalid key accepted")
	}
}
func TestIntegrationRedisStreamsRetentionAndRecovery(t *testing.T) {
	addr := integrationEnv(t, "INTEGRATION_REDIS_URL")
	key := "wpp:test:" + uuid.NewString()
	a, err := NewRedisStream(addr, key, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer a.client.Del(context.Background(), key)
	// A wrong-type key reproduces a real Redis rejection without disconnecting other tests.
	if err = a.client.Set(ctx, key, "wrong-type", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	e := Event{ID: uuid.NewString(), Session: "alpha", Name: "onmessage", Created: 123, Data: []byte(`{"session":"alpha","event":"onmessage"}`)}
	if err = a.Deliver(ctx, e); err == nil {
		t.Fatal("reported a rejected append as delivered")
	}
	if err = a.client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	if err = a.Deliver(ctx, e); err != nil {
		t.Fatal(err)
	}
	b, err := NewRedisStream(addr, key, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.Deliver(ctx, e); err != nil {
		t.Fatal(err)
	}
	rows, err := a.client.XRange(ctx, key, "-", "+").Result()
	if err != nil || len(rows) != 2 {
		t.Fatal("missing replay", err)
	}
	for _, row := range rows {
		if row.Values["eventId"] != e.ID || row.Values["session"] != "alpha" || row.Values["payload"] != string(e.Data) {
			t.Fatal("identity or payload changed")
		}
	}
	next := e
	next.ID = uuid.NewString()
	if err = b.Deliver(ctx, next); err != nil {
		t.Fatal(err)
	}
	rows, err = a.client.XRange(ctx, key, "-", "+").Result()
	if err != nil || len(rows) != 2 || rows[1].Values["eventId"] != next.ID {
		t.Fatal("stream retention failed", err)
	}
}
