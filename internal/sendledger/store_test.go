package sendledger

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "sends.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestOnlyOneConcurrentReservationAcrossConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sends.db")
	a, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	var fresh atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := a
			if i%2 == 0 {
				s = b
			}
			_, ok, err := s.Claim(context.Background(), "alpha", "key", "payload")
			if err != nil {
				t.Error(err)
			}
			if ok {
				fresh.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if fresh.Load() != 1 {
		t.Fatalf("executed %d times", fresh.Load())
	}
}
func TestResultSurvivesRestartAndAmbiguousReservationsNeverExpire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sends.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	for _, key := range []string{"pending", "ambiguous", "complete"} {
		r, ok, err := s.Claim(ctx, "alpha", key, key)
		if err != nil || !ok {
			t.Fatal(err)
		}
		if key != "pending" {
			code := 201
			if key == "ambiguous" {
				code = 504
			}
			if err = s.Finish(ctx, "alpha", key, r.Owner, code, "application/json", []byte(`{"id":"accepted"}`)); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.Get(ctx, "alpha", "complete")
	if e != nil || r.Code != 201 || string(r.Body) != `{"id":"accepted"}` {
		t.Fatalf("lost result: %+v %v", r, e)
	}
	_, e = s.db.Exec("UPDATE sends SET created=?", time.Now().Add(-30*24*time.Hour).Unix())
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"pending", "ambiguous"} {
		_, fresh, e := s.Claim(ctx, "alpha", key, key)
		if e != nil || fresh {
			t.Fatalf("unsafe retry: %s %v", key, e)
		}
	}
	if _, e = s.Get(ctx, "alpha", "complete"); !errors.Is(e, ErrMissing) {
		t.Fatal("complete retention was not bounded", e)
	}
}
func TestSessionScopeFingerprintCapacityAndOwnership(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.Limit = 2
	r, _, e := s.Claim(ctx, "alpha", "key", "payload")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Claim(ctx, "alpha", "key", "changed"); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, _, e = s.Claim(ctx, "beta", "key", "changed"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Claim(ctx, "gamma", "key", "new"); !errors.Is(e, ErrFull) {
		t.Fatal(e)
	}
	if e = s.Finish(ctx, "alpha", "key", "wrong-owner", 201, "application/json", nil); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if e = s.Finish(ctx, "alpha", "key", r.Owner, 201, "application/json", []byte(`{"id":"one"}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(ctx, "alpha", "key", r.Owner, 201, "application/json", []byte(`{"id":"two"}`)); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, "gamma", "key"); !errors.Is(e, ErrMissing) {
		t.Fatal(e)
	}
}
func TestReserveResponseCapacityBeforeSending(t *testing.T) {
	s := testStore(t)
	s.MaxBytes = MaxResponse
	ctx := context.Background()
	r, _, e := s.Claim(ctx, "alpha", "one", "payload")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Claim(ctx, "alpha", "two", "payload"); !errors.Is(e, ErrFull) {
		t.Fatal(e)
	}
	if e = s.Finish(ctx, "alpha", "one", r.Owner, 201, "application/json", []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Claim(ctx, "alpha", "two", "payload"); !errors.Is(e, ErrFull) {
		t.Fatal("must reserve full maximum", e)
	}
	s.MaxBytes += 2
	if _, _, e = s.Claim(ctx, "alpha", "two", "payload"); e != nil {
		t.Fatal(e)
	}
}
