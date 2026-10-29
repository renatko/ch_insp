package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Parse returns the connection URL. Explicit flags override CLICKHOUSE_URL.
func Parse(args []string, envURL string, output io.Writer) (string, error) {
	fs := flag.NewFlagSet("ch_insp", flag.ContinueOnError)
	fs.SetOutput(output)
	dsn := fs.String("url", "", "ClickHouse connection URL (default: CLICKHOUSE_URL)")
	host := fs.String("host", "", "ClickHouse host (default: localhost)")
	port := fs.Int("port", 0, "ClickHouse native TCP port (default: 9000)")
	user := fs.String("user", "", "ClickHouse username (default: default)")
	password := fs.String("password", "", "ClickHouse password (may be visible in process arguments)")
	database := fs.String("database", "", "initial database (default: default)")
	secure := fs.Bool("secure", false, "use TLS for the connection")
	fs.Usage = func() { fmt.Fprintln(output, "Usage: ch_insp [options]"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() != 0 {
		return "", fmt.Errorf("unexpected positional argument")
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if !set["url"] {
		*dsn = envURL
	}
	if strings.TrimSpace(*dsn) == "" {
		if len(set) == 0 || (len(set) == 1 && set["url"]) {
			return "", nil
		}
		*dsn = "clickhouse://localhost:9000/default"
	}
	u, err := url.Parse(*dsn)
	if err != nil {
		var parseErr *url.Error
		if errors.As(err, &parseErr) {
			return "", fmt.Errorf("invalid ClickHouse URL: %w", parseErr.Err)
		}
		return "", fmt.Errorf("invalid ClickHouse URL")
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid ClickHouse URL: expected scheme and host")
	}
	if set["host"] || set["port"] {
		name, portValue := u.Hostname(), u.Port()
		if set["host"] {
			name = strings.TrimSpace(*host)
			name = strings.TrimPrefix(strings.TrimSuffix(name, "]"), "[")
			if name == "" || (strings.Contains(name, ":") && net.ParseIP(name) == nil) {
				return "", fmt.Errorf("invalid --host: use a hostname or IP address without a port")
			}
		}
		if set["port"] {
			if *port < 1 || *port > 65535 {
				return "", fmt.Errorf("invalid --port: must be between 1 and 65535")
			}
			portValue = strconv.Itoa(*port)
		}
		if portValue == "" {
			u.Host = name
			if strings.Contains(name, ":") {
				u.Host = "[" + name + "]"
			}
		} else {
			u.Host = net.JoinHostPort(name, portValue)
		}
	}
	if set["user"] || set["password"] {
		name, pass, hasPass := "default", "", false
		if u.User != nil {
			name = u.User.Username()
			pass, hasPass = u.User.Password()
		}
		if set["user"] {
			name = *user
		}
		if set["password"] {
			pass, hasPass = *password, true
		}
		if hasPass {
			u.User = url.UserPassword(name, pass)
		} else {
			u.User = url.User(name)
		}
	}
	if set["database"] {
		if *database == "" {
			return "", fmt.Errorf("--database cannot be empty")
		}
		u.Path = "/" + *database
		u.RawPath = "/" + url.PathEscape(*database)
	}
	if set["secure"] {
		q := u.Query()
		q.Set("secure", strconv.FormatBool(*secure))
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
