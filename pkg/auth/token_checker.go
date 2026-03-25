package auth

import (
	"fmt"
	"sync"
)

// TokenChecker maintains an in-memory set of revoked JTIs for O(1) lookup.
// It is populated from a TokenStore on startup and updated synchronously on
// each revocation so that every subsequent request is rejected immediately.
type TokenChecker struct {
	mu      sync.RWMutex
	revoked map[string]struct{}
}

// NewTokenChecker creates a TokenChecker and pre-loads revoked JTIs from
// store. If store is nil an empty (no revocations known) checker is returned.
func NewTokenChecker(store *TokenStore) (*TokenChecker, error) {
	c := &TokenChecker{
		revoked: make(map[string]struct{}),
	}
	if store == nil {
		return c, nil
	}
	ids, err := store.LoadRevoked()
	if err != nil {
		return nil, fmt.Errorf("auth: init token checker: %w", err)
	}
	for _, id := range ids {
		c.revoked[id] = struct{}{}
	}
	return c, nil
}

// IsRevoked reports whether the given JTI has been revoked.
func (c *TokenChecker) IsRevoked(jti string) bool {
	if jti == "" {
		return false
	}
	c.mu.RLock()
	_, ok := c.revoked[jti]
	c.mu.RUnlock()
	return ok
}

// MarkRevoked adds a JTI to the in-memory revoked set. Callers should also
// persist the revocation to the TokenStore.
func (c *TokenChecker) MarkRevoked(jti string) {
	c.mu.Lock()
	c.revoked[jti] = struct{}{}
	c.mu.Unlock()
}
