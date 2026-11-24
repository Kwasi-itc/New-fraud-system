package auth

import (
	"container/heap"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"sync"
	"time"
)

var ErrReplay = errors.New("client assertion already consumed")
var ErrReplayCapacity = errors.New("client assertion replay cache is full")

// ReplayCache is deliberately process-local. Restarts lose used assertion IDs.
// A full cache fails closed rather than evicting a still-valid assertion.
type replayEntry struct {
	key    [32]byte
	expiry time.Time
	index  int
}
type replayHeap []*replayEntry

func (h replayHeap) Len() int           { return len(h) }
func (h replayHeap) Less(i, j int) bool { return h[i].expiry.Before(h[j].expiry) }
func (h replayHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *replayHeap) Push(value any) {
	e := value.(*replayEntry)
	e.index = len(*h)
	*h = append(*h, e)
}
func (h *replayHeap) Pop() any {
	old := *h
	e := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	return e
}

type ReplayCache struct {
	mu      sync.Mutex
	entries map[[32]byte]*replayEntry
	expiry  replayHeap
	max     int
}

func NewReplayCache(maxEntries int) *ReplayCache {
	if maxEntries < 1 {
		panic("positive replay cache capacity required")
	}
	return &ReplayCache{entries: make(map[[32]byte]*replayEntry), max: maxEntries}
}
func (c *ReplayCache) Consume(client, jti string, until, now time.Time) error {
	if c == nil || client == "" || jti == "" || len(jti) > 256 || !until.After(now) {
		return errors.New("invalid replay cache claim")
	}
	key := sha256.Sum256([]byte(client + "\x00" + jti))
	c.mu.Lock()
	defer c.mu.Unlock()
	// Bound expiration cleanup per request; live proofs are never evicted.
	for removed := 0; removed < 128 && len(c.expiry) > 0 && !c.expiry[0].expiry.After(now); removed++ {
		expired := heap.Pop(&c.expiry).(*replayEntry)
		delete(c.entries, expired.key)
	}
	if previous, ok := c.entries[key]; ok {
		if previous.expiry.After(now) {
			return ErrReplay
		}
		previous.expiry = until
		heap.Fix(&c.expiry, previous.index)
		return nil
	}
	if len(c.entries) >= c.max {
		return ErrReplayCapacity
	}
	entry := &replayEntry{key: key, expiry: until}
	heap.Push(&c.expiry, entry)
	c.entries[key] = entry
	return nil
}

func PublicKeyFingerprint(pem []byte) (string, error) {
	key, err := jwt.ParseRSAPublicKeyFromPEM(pem)
	if err != nil || key.N.BitLen() < 2048 {
		return "", errors.New("RSA public key must be at least 2048 bits")
	}
	// Canonical modulus and exponent prevent the same key with different PEM encodings.
	sum := sha256.Sum256(append(key.N.Bytes(), byte(key.E>>24), byte(key.E>>16), byte(key.E>>8), byte(key.E)))
	return hex.EncodeToString(sum[:]), nil
}
func VerifyClientAssertion(raw, client, audience, kid string, key *rsa.PublicKey, now time.Time) (*jwt.RegisteredClaims, error) {
	invalid := errors.New("invalid client assertion")
	if len(raw) == 0 || len(raw) > 16384 || client == "" || audience == "" || kid == "" || key == nil || key.N.BitLen() < 2048 {
		return nil, invalid
	}
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		id, _ := t.Header["kid"].(string)
		if id != kid {
			return nil, invalid
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(client), jwt.WithSubject(client), jwt.WithAudience(audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(func() time.Time { return now }), jwt.WithLeeway(ClockLeeway))
	if err != nil || !token.Valid || claims.IssuedAt == nil || claims.ExpiresAt == nil || len(claims.Audience) != 1 || claims.ID == "" || len(claims.ID) > 256 || claims.ExpiresAt.Sub(claims.IssuedAt.Time) > time.Minute || !claims.ExpiresAt.After(claims.IssuedAt.Time) || claims.IssuedAt.After(now.Add(ClockLeeway)) {
		return nil, invalid
	}
	return claims, nil
}
