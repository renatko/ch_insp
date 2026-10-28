package clickhouse

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"time"
)

// Log-derived history/status is refreshed less often than the 60-second
// rates. Cache keys contain only a digest, never the connection URL.
type cacheEntry[T any] struct {
	value T
	until time.Time
}
type metricCache[T any] struct {
	mu      sync.Mutex
	entries map[[32]byte]cacheEntry[T]
}

func (c *metricCache[T]) get(key [32]byte) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if ok && time.Now().Before(entry.until) {
		return entry.value, true
	}
	var zero T
	return zero, false
}
func (c *metricCache[T]) put(key [32]byte, value T, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[[32]byte]cacheEntry[T])
	}
	now := time.Now()
	if len(c.entries) >= 128 {
		for k, e := range c.entries {
			if !now.Before(e.until) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= 128 {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
	}
	c.entries[key] = cacheEntry[T]{value: value, until: now.Add(ttl)}
}
func metricCacheKey(dsn string, names []string, period int) [32]byte {
	h := sha256.New()
	var length [8]byte
	for _, s := range append([]string{dsn}, names...) {
		binary.BigEndian.PutUint64(length[:], uint64(len(s)))
		h.Write(length[:])
		h.Write([]byte(s))
	}
	binary.BigEndian.PutUint64(length[:], uint64(period))
	h.Write(length[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

var statusCache metricCache[map[string]ViewStatus]
var viewHistoryCache metricCache[map[string]ViewHistory]
var tableHistoryCache metricCache[map[string][5]uint64]

func metricWindowKey(dsn string, names []string, period int, end time.Time) [32]byte {
	base := metricCacheKey(dsn, names, period)
	var value [40]byte
	copy(value[:32], base[:])
	binary.BigEndian.PutUint64(value[32:], uint64(end.Unix()))
	return sha256.Sum256(value[:])
}
