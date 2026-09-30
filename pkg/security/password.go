package security

import (
	"crypto/subtle"
	"errors"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// bcrypt only considers the first 72 bytes of input; longer passwords are
// rejected rather than silently truncated.
const maxPasswordBytes = 72

var errPasswordTooLong = errors.New("password must be at most 72 bytes")

func hashPassword(password string) (string, error) {
	if len(password) > maxPasswordBytes {
		return "", errPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

func isBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}

// verifyPassword checks supplied against the stored value. A stored bcrypt hash
// is compared with bcrypt. A legacy cleartext value (written before hashing was
// implemented) is compared in constant time and, on a match, needsRehash is true
// so the caller can upgrade the row to a bcrypt hash. An empty stored value
// (e.g. an OAuth2-only user) never matches.
func verifyPassword(stored, supplied string) (ok, needsRehash bool) {
	if stored == "" || supplied == "" || len(supplied) > maxPasswordBytes {
		return false, false
	}
	if isBcryptHash(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(supplied)) == nil, false
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(supplied)) == 1 {
		return true, true
	}
	return false, false
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// burnPasswordCheck spends roughly one bcrypt comparison so an unknown username
// costs about the same as a wrong password.
func burnPasswordCheck(supplied string) {
	dummyHashOnce.Do(func() {
		h, _ := bcrypt.GenerateFromPassword([]byte("resolvespec-dummy"), bcrypt.DefaultCost)
		dummyHash = string(h)
	})
	if len(supplied) > maxPasswordBytes {
		supplied = supplied[:maxPasswordBytes]
	}
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(supplied))
}
