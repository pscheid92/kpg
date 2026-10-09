package kpg

import (
	"fmt"
	"io"
	"sync"
)

func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func writeln(w io.Writer, args ...any) error {
	_, err := fmt.Fprintln(w, args...)
	return err
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// tableWidths holds the column widths of the target tables printed by list
// and by the pickers.
type tableWidths struct {
	Target   int
	Provider int
	Database int
	User     int
}

func newTableWidths(target, provider, database, user string) tableWidths {
	return tableWidths{Target: len(target), Provider: len(provider), Database: len(database), User: len(user)}
}

func (w *tableWidths) fit(target, provider, database, user string) {
	w.Target = max(w.Target, len(target))
	w.Provider = max(w.Provider, len(provider))
	w.Database = max(w.Database, len(database))
	w.User = max(w.User, len(user))
}

// lockedWriter serializes status messages that several goroutines write to
// the same stream.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
