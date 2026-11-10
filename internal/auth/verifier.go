package auth

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"time"
)

// Verifier reuses a recent successful bcrypt decision only for the exact
// session, secret and token bytes. Fixed-size slots bound memory regardless of
// request volume. A collision or expiry falls back to full verification.
type Verifier struct {
	mu    sync.RWMutex
	slots [256]verificationSlot
}
type verificationSlot struct {
	digest  [32]byte
	expires time.Time
}

func credentialDigest(session, secret, token string) [32]byte {
	h := sha256.New()
	var size [8]byte
	for _, v := range []string{session, secret, token} {
		binary.BigEndian.PutUint64(size[:], uint64(len(v)))
		h.Write(size[:])
		h.Write([]byte(v))
	}
	var digest [32]byte
	h.Sum(digest[:0])
	return digest
}
func (v *Verifier) Verify(session, secret, token string) error {
	return v.verifyAt(time.Now(), session, secret, token, Verify)
}
func (v *Verifier) verifyAt(now time.Time, session, secret, token string, verify func(string, string, string) error) error {
	d := credentialDigest(session, secret, token)
	idx := d[0]
	v.mu.RLock()
	slot := v.slots[idx]
	v.mu.RUnlock()
	if slot.digest == d && now.Before(slot.expires) {
		return nil
	}
	// Never hold the cache lock across bcrypt. Concurrent cold verifications can
	// both run, but neither can populate a slot without independently succeeding.
	if err := verify(session, secret, token); err != nil {
		return err
	}
	v.mu.Lock()
	v.slots[idx] = verificationSlot{d, now.Add(30 * time.Second)}
	v.mu.Unlock()
	return nil
}
