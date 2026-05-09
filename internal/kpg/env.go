package kpg

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func RenderEnv(w io.Writer, format string, values EnvValues) error {
	if format == "" {
		format = DefaultOutput
	}
	switch format {
	case "shell":
		if err := writef(w, "export PGHOST=%s\n", ShellQuote(values.Host)); err != nil {
			return err
		}
		if err := writef(w, "export PGPORT=%s\n", ShellQuote(strconv.Itoa(values.Port))); err != nil {
			return err
		}
		if values.User != "" {
			if err := writef(w, "export PGUSER=%s\n", ShellQuote(values.User)); err != nil {
				return err
			}
		}
		if values.Password != "" {
			if err := writef(w, "export PGPASSWORD=%s\n", ShellQuote(values.Password)); err != nil {
				return err
			}
		}
		if values.Database != "" {
			if err := writef(w, "export PGDATABASE=%s\n", ShellQuote(values.Database)); err != nil {
				return err
			}
		}
		if err := writef(w, "export PGSSLMODE=%s\n", ShellQuote(values.sslMode())); err != nil {
			return err
		}
	case "dotenv":
		if err := writef(w, "PGHOST=%s\n", DotenvQuote(values.Host)); err != nil {
			return err
		}
		if err := writef(w, "PGPORT=%s\n", DotenvQuote(strconv.Itoa(values.Port))); err != nil {
			return err
		}
		if values.User != "" {
			if err := writef(w, "PGUSER=%s\n", DotenvQuote(values.User)); err != nil {
				return err
			}
		}
		if values.Password != "" {
			if err := writef(w, "PGPASSWORD=%s\n", DotenvQuote(values.Password)); err != nil {
				return err
			}
		}
		if values.Database != "" {
			if err := writef(w, "PGDATABASE=%s\n", DotenvQuote(values.Database)); err != nil {
				return err
			}
		}
		if err := writef(w, "PGSSLMODE=%s\n", DotenvQuote(values.sslMode())); err != nil {
			return err
		}
	case "json":
		values.SSLMode = values.sslMode()
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(values)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
	return nil
}

func (values EnvValues) sslMode() string {
	if values.SSLMode != "" {
		return values.SSLMode
	}
	return DefaultSSLMode
}

func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return (r < 'A' || r > 'Z') &&
			(r < 'a' || r > 'z') &&
			(r < '0' || r > '9') &&
			!strings.ContainsRune("@%_+=:,./-", r)
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func DotenvQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\r\n#'\"") {
		return s
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + replacer.Replace(s) + `"`
}
