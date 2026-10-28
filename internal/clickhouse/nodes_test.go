package clickhouse

import (
	"fmt"
	"strings"
	"testing"
)

func TestNodeDSNPreservesCredentialsAndSettings(t *testing.T) {
	dsn, err := NodeDSN("clickhouse://user:secret@original:9000/db?secure=true", Node{Host: "2001:db8::1", Port: 9440})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"user:secret@", "[2001:db8::1]:9440", "/db", "secure=true"} {
		if !strings.Contains(dsn, part) {
			t.Fatalf("missing %s", part)
		}
	}
	if _, err = NodeDSN("http://user:secret@host:8123/db", Node{Host: "other", Port: 9000}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("HTTP switch should fail without revealing credentials")
	}
	if _, err = NodeDSN("clickhouse://user:secret@host/db", Node{}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid node should fail without revealing credentials")
	}
}

func TestNodeConnectionErrorRedactsCredentials(t *testing.T) {
	dsn := "clickhouse://user:p%40ss@host:9000/db"
	err := RedactConnectionError(fmt.Errorf("connect %s: password p@ss rejected", dsn), dsn)
	if strings.Contains(err.Error(), dsn) || strings.Contains(err.Error(), "p@ss") || !strings.Contains(err.Error(), "rejected") {
		t.Fatal("connection error was not safely redacted")
	}
}
