package config

import (
	"bytes"
	"flag"
	"net/url"
	"strings"
	"testing"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

func TestConnectionFlagsOverrideURL(t *testing.T) {
	dsn, err := Parse([]string{"--host", "example.com", "--port", "9440", "--user", "new user", "--password", "p@ss:/word", "--database", "analytics", "--secure"}, "clickhouse://old:secret@localhost:9000/default?dial_timeout=5s", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Addr) != 1 || parsed.Addr[0] != "example.com:9440" || parsed.Auth.Username != "new user" || parsed.Auth.Password != "p@ss:/word" || parsed.Auth.Database != "analytics" || parsed.TLS == nil {
		t.Fatalf("unexpected parsed configuration: %#v", parsed)
	}
	u, _ := url.Parse(dsn)
	if u.Query().Get("dial_timeout") != "5s" {
		t.Fatal("existing URL options were not preserved")
	}
}

func TestURLPrecedenceAndDefaults(t *testing.T) {
	out := &bytes.Buffer{}
	dsn, err := Parse([]string{"--url", "clickhouse://a:9001/db", "--user", "reader"}, "clickhouse://other:9000/default", out)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Addr[0] != "a:9001" || parsed.Auth.Username != "reader" || parsed.Auth.Database != "db" {
		t.Fatalf("unexpected DSN: %#v", parsed)
	}
	dsn, err = Parse([]string{"--user", "reader"}, "", out)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = clickhouse.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Addr[0] != "localhost:9000" || parsed.Auth.Database != "default" {
		t.Fatalf("unexpected defaults: %#v", parsed)
	}
	dsn, err = Parse(nil, "", out)
	if err != nil || dsn != "" {
		t.Fatalf("unexpected empty config: %q %v", dsn, err)
	}
}

func TestIPv6AndPreservingCredentials(t *testing.T) {
	dsn, err := Parse([]string{"--host", "2001:db8::1", "--port", "9000"}, "clickhouse://bob:secret@host:8123/db", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	if u.Host != "[2001:db8::1]:9000" || u.User.Username() != "bob" || pass != "secret" {
		t.Fatalf("wrong override: %s", u.Redacted())
	}
}

func TestInvalidOptionsAndSecretRedaction(t *testing.T) {
	for _, args := range [][]string{{"--port", "0"}, {"--port", "65536"}, {"--host", "server:9000"}, {"--database", ""}} {
		if _, err := Parse(args, "", &bytes.Buffer{}); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}
	_, err := Parse([]string{"--url", "clickhouse://user:SECRET@host/%zz"}, "", &bytes.Buffer{})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("URL error leaked credentials: %v", err)
	}
	help := &bytes.Buffer{}
	_, err = Parse([]string{"--help"}, "clickhouse://user:SECRET@localhost:9000/default", help)
	if err != flag.ErrHelp {
		t.Fatalf("help error = %v", err)
	}
	if strings.Contains(help.String(), "SECRET") {
		t.Fatal("help output leaked credentials")
	}
}

func TestSQLDumpOptions(t *testing.T) {
	opts, err := ParseOptions([]string{"--schema", "schema.sql", "--database", "shop"}, "clickhouse://ignored:9000/default", &bytes.Buffer{})
	if err != nil || opts.SchemaPath != "schema.sql" || opts.Database != "shop" || opts.DSN != "" {
		t.Fatalf("incorrect offline options: %#v %v", opts, err)
	}
	for _, args := range [][]string{{"--schema", ""}, {"--schema", "schema.sql", "--host", "localhost"}, {"--schema", "schema.sql", "--url", "clickhouse://localhost:9000"}} {
		if _, err := ParseOptions(args, "", &bytes.Buffer{}); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}
}
