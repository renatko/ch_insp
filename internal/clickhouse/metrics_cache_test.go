package clickhouse

import (
	"testing"
	"time"
)

func TestMetricCacheIsolationAndExpiry(t *testing.T) {
	var c metricCache[int]
	key := metricCacheKey("clickhouse://u:secret@host/db", []string{"ab", "c"}, 60)
	if key == metricCacheKey("clickhouse://u:secret@host/db", []string{"a", "bc"}, 60) || key == metricCacheKey("clickhouse://u:secret@host/db", []string{"ab", "c"}, 300) {
		t.Fatal("cache keys collide across names or periods")
	}
	c.put(key, 42, time.Minute)
	if v, ok := c.get(key); !ok || v != 42 {
		t.Fatal("cached value missing")
	}
	c.put(key, 42, -time.Second)
	if _, ok := c.get(key); ok {
		t.Fatal("expired cache value returned")
	}
}

func TestMetricWindowCacheKeyChangesAtBoundary(t *testing.T) {
	end := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC)
	first := metricWindowKey("dsn", []string{"db.mv"}, 60, end)
	if first == metricWindowKey("dsn", []string{"db.mv"}, 60, end.Add(time.Minute)) {
		t.Fatal("cache key reused across chart windows")
	}
	if first != metricWindowKey("dsn", []string{"db.mv"}, 60, end) {
		t.Fatal("same window did not use same cache key")
	}
}
