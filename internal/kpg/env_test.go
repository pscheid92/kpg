package kpg

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRenderEnv(t *testing.T) {
	var buf bytes.Buffer
	err := RenderEnv(&buf, "shell", EnvValues{
		Host:     "127.0.0.1",
		Port:     15432,
		User:     "app",
		Password: "pa ss'word",
		Database: "appdb",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "export PGHOST=127.0.0.1\nexport PGPORT=15432\nexport PGUSER=app\nexport PGPASSWORD='pa ss'\\''word'\nexport PGDATABASE=appdb\nexport PGSSLMODE=disable\n"
	if buf.String() != want {
		t.Fatalf("unexpected shell env:\n%s", buf.String())
	}

	buf.Reset()
	err = RenderEnv(&buf, "json", EnvValues{Host: "127.0.0.1", Port: 15432, User: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"PGHOST": "127.0.0.1"`) || !strings.Contains(buf.String(), `"PGPORT": 15432`) || !strings.Contains(buf.String(), `"PGSSLMODE": "disable"`) {
		t.Fatalf("unexpected json env:\n%s", buf.String())
	}
}

func TestRenderEnvDotenvAndDefaultFormat(t *testing.T) {
	values := EnvValues{
		Host:     "localhost",
		Port:     5432,
		User:     "app user",
		Password: "line1\nline2",
		Database: "app#db",
		SSLMode:  "require",
	}

	var buf bytes.Buffer
	if err := RenderEnv(&buf, "dotenv", values); err != nil {
		t.Fatal(err)
	}
	want := "PGHOST=localhost\nPGPORT=5432\nPGUSER=\"app user\"\nPGPASSWORD=\"line1\\nline2\"\nPGDATABASE=\"app#db\"\nPGSSLMODE=require\n"
	if buf.String() != want {
		t.Fatalf("unexpected dotenv env:\n%s", buf.String())
	}

	buf.Reset()
	if err := RenderEnv(&buf, "", EnvValues{Host: "127.0.0.1", Port: 5432}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "export PGHOST=127.0.0.1") || !strings.Contains(got, "export PGSSLMODE=disable") {
		t.Fatalf("unexpected default env:\n%s", got)
	}
}

func TestRenderEnvRejectsUnsupportedFormat(t *testing.T) {
	err := RenderEnv(io.Discard, "yaml", EnvValues{})
	if err == nil || !strings.Contains(err.Error(), `unsupported output format "yaml"`) {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}

func TestEnvQuoting(t *testing.T) {
	shellCases := map[string]string{
		"":              "''",
		"plain_value-1": "plain_value-1",
		"has space":     "'has space'",
		"can't":         "'can'\\''t'",
	}
	for input, want := range shellCases {
		if got := ShellQuote(input); got != want {
			t.Fatalf("ShellQuote(%q) = %q, want %q", input, got, want)
		}
	}

	dotenvCases := map[string]string{
		"":           `""`,
		"plain":      "plain",
		"has space":  `"has space"`,
		"has#hash":   `"has#hash"`,
		"has\"quote": `"has\"quote"`,
		"line1\n2":   `"line1\n2"`,
		`c:\tmp`:     `c:\tmp`,
	}
	for input, want := range dotenvCases {
		if got := DotenvQuote(input); got != want {
			t.Fatalf("DotenvQuote(%q) = %q, want %q", input, got, want)
		}
	}
}
