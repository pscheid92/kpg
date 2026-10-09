package kpg

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type ExitError struct {
	Code int
}

func (e ExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", e.Code)
}

func (e ExitError) ExitCode() int {
	return e.Code
}

// exitCodeInterrupted follows the shell convention for a process that was
// stopped with Ctrl-C.
const exitCodeInterrupted = 130

func resolveShell() (string, error) {
	if runtime.GOOS == "windows" {
		return lookupShell(os.Getenv("SHELL"), "pwsh", "powershell", os.Getenv("COMSPEC"), "cmd")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	info, err := os.Stat(shell) //nolint:gosec // the path is the user's own SHELL setting
	if err != nil {
		return "", fmt.Errorf("could not start shell %q: %w", shell, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("could not start shell %q: is a directory", shell)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("could not start shell %q: not executable", shell)
	}
	return shell, nil
}

// lookupShell returns the first candidate that resolves to an executable.
// Windows has no executable permission bit, so PATH lookup replaces the
// permission checks used on Unix.
func lookupShell(candidates ...string) (string, error) {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("could not find a shell: set SHELL or COMSPEC to an executable")
}

// runChild runs the client command or subshell with the connection exported
// in its environment and returns its exit status.
//
// The terminal delivers Ctrl-C to the whole foreground process group, so the
// child receives SIGINT directly and handles it itself: psql cancels the
// running query, a shell prints a fresh prompt. kpg must survive that signal
// while the child runs, which is why the caller keeps a handler installed and
// passes the signals here. SIGTERM is forwarded so that killing kpg also
// stops the child and lets the tunnel close.
func runChild(sigs <-chan os.Signal, args []string, t Target, values EnvValues, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = childEnv(os.Environ(), t, values)
	if err := cmd.Start(); err != nil {
		return err
	}
	waited := make(chan error, 1)
	go func() {
		waited <- cmd.Wait()
	}()
	for {
		select {
		case sig := <-sigs:
			if sig == syscall.SIGTERM {
				_ = cmd.Process.Signal(sig)
			}
		case err := <-waited:
			return childExitError(err)
		}
	}
}

func childExitError(err error) error {
	if err == nil {
		return nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return ExitError{Code: exitErr.ExitCode()}
	}
	return err
}

// conflictingPGEnv lists libpq variables that would silently override the
// tunnel: a connection service definition or an explicit host address.
var conflictingPGEnv = []string{"PGSERVICE", "PGSERVICEFILE", "PGHOSTADDR"}

// childEnv builds the child's environment: the inherited variables minus
// anything kpg sets itself or that would conflict with the tunnel, plus the
// PG* values for the connection.
func childEnv(env []string, t Target, values EnvValues) []string {
	drop := map[string]bool{
		"PGHOST": true, "PGPORT": true, "PGUSER": true, "PGPASSWORD": true, "PGDATABASE": true, "PGSSLMODE": true,
		"KPG_TARGET": true, "KPG_PROVIDER": true,
	}
	for _, name := range conflictingPGEnv {
		drop[name] = true
	}
	result := make([]string, 0, len(env)+8)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if drop[name] {
			continue
		}
		result = append(result, entry)
	}
	result = append(result,
		"PGHOST="+values.Host,
		"PGPORT="+strconv.Itoa(values.Port),
		"PGSSLMODE="+values.sslMode(),
		"KPG_TARGET="+t.ID(),
	)
	if t.Provider != "" {
		result = append(result, "KPG_PROVIDER="+t.Provider)
	}
	if values.User != "" {
		result = append(result, "PGUSER="+values.User)
	}
	if values.Password != "" {
		result = append(result, "PGPASSWORD="+values.Password)
	}
	if values.Database != "" {
		result = append(result, "PGDATABASE="+values.Database)
	}
	return result
}
