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
