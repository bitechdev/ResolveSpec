package resolvemcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// confirmStore holds the single-use confirmation tokens for filter-based writes. It is
// in-memory: tokens are lost on restart and are not shared between instances, which only costs
// the client one more preview call.
type confirmStore struct {
	mu      sync.Mutex
	tokens  map[string]confirmEntry
	now     func() time.Time
	maxLive int
}

type confirmEntry struct {
	user, table, op, binding string
	expires                  time.Time
}

func newConfirmStore() *confirmStore {
	return &confirmStore{tokens: map[string]confirmEntry{}, now: time.Now, maxLive: 10000}
}

var errConfirmInvalid = NewClientError(CodeInvalidArgument, "confirm_token is invalid or expired; repeat the call without it to get a new preview")

// issue returns a token bound to the caller, table, operation and binding (a hash of the
// filters, data and matched rows the preview showed).
func (c *confirmStore) issue(user, table, op, binding string, ttl time.Duration) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, e := range c.tokens {
		if now.After(e.expires) {
			delete(c.tokens, k)
		}
	}
	if len(c.tokens) >= c.maxLive {
		return "", NewClientError(CodeLimitExceeded, "too many pending confirmations; try again later")
	}
	c.tokens[tok] = confirmEntry{user: user, table: table, op: op, binding: binding, expires: now.Add(ttl)}
	return tok, nil
}

// consume validates and removes a token. Any mismatch (other user, table, operation or
// changed binding) is the same error, and the token is spent either way.
func (c *confirmStore) consume(tok, user, table, op, binding string) error {
	c.mu.Lock()
	e, ok := c.tokens[tok]
	delete(c.tokens, tok)
	now := c.now()
	c.mu.Unlock()
	if !ok || now.After(e.expires) || e.user != user || e.table != table || e.op != op || e.binding != binding {
		return errConfirmInvalid
	}
	return nil
}

// bindingHash fingerprints the parts of a write a confirmation covers.
func bindingHash(parts ...any) (string, error) {
	h := sha256.New()
	for _, p := range parts {
		b, err := json.Marshal(p)
		if err != nil {
			return "", errors.New("cannot fingerprint request")
		}
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
