package auth

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestVerifierBindsAllInputsExpiresAndNeverCachesFailures(t *testing.T) {
	var v Verifier
	start := time.Unix(1000, 0)
	calls := 0
	check := func(s, k, tok string) error {
		calls++
		if s != "alpha" || k != "secret" || tok != "token" {
			return errors.New("denied")
		}
		return nil
	}
	run := func(dt time.Duration, s, k, tok string, ok bool, wantCalls int) {
		t.Helper()
		if err := v.verifyAt(start.Add(dt), s, k, tok, check); (err == nil) != ok || calls != wantCalls {
			t.Fatalf("unexpected verification or verification count %d/%d", calls, wantCalls)
		}
	}
	run(0, "alpha", "secret", "token", true, 1)
	run(time.Second, "alpha", "secret", "token", true, 1)
	run(time.Second, "beta", "secret", "token", false, 2)
	run(time.Second, "alpha", "rotated", "token", false, 3)
	run(time.Second, "alpha", "secret", "other", false, 4)
	run(time.Second, "alpha", "secret", "other", false, 5)
	run(29*time.Second, "alpha", "secret", "token", true, 5)
	run(30*time.Second, "alpha", "secret", "token", true, 6)
	if credentialDigest("ab", "c", "d") == credentialDigest("a", "bc", "d") {
		t.Fatal("ambiguous credential encoding")
	}
}
func TestVerifierConcurrentSessionIsolation(t *testing.T) {
	var v Verifier
	token, e := Generate("alpha", "secret")
	if e != nil {
		t.Fatal(e)
	}
	if e = v.Verify("alpha", "secret", token); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := "alpha"
			if i%3 == 0 {
				s = "beta"
			}
			if e := v.Verify(s, "secret", token); (e == nil) != (s == "alpha") {
				t.Error("cross-session cache decision")
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkSessionVerification(b *testing.B) {
	token, err := Generate("bench", "synthetic-secret")
	if err != nil {
		b.Fatal(err)
	}
	b.Run("bcrypt", func(b *testing.B) {
		for b.Loop() {
			if err := Verify("bench", "synthetic-secret", token); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("warm", func(b *testing.B) {
		var v Verifier
		if err := v.Verify("bench", "synthetic-secret", token); err != nil {
			b.Fatal(err)
		}
		for b.Loop() {
			if err := v.Verify("bench", "synthetic-secret", token); err != nil {
				b.Fatal(err)
			}
		}
	})
}
