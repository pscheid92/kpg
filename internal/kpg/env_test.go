package kpg

import (
	"bytes"
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
