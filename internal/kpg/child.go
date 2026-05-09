package kpg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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

func resolveShell() (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	info, err := os.Stat(shell)
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

func runClient(ctx context.Context, args []string, t Target, values EnvValues, stdout io.Writer, stderr io.Writer) error {
	return runChild(ctx, args, t, values, stdout, stderr)
}

func runChild(ctx context.Context, args []string, t Target, values EnvValues, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = appendPGEnv(os.Environ(), t, values)
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return ExitError{Code: exitErr.ExitCode()}
	}
	return err
}

func appendPGEnv(env []string, t Target, values EnvValues) []string {
	env = append(env,
		"PGHOST="+values.Host,
		"PGPORT="+strconv.Itoa(values.Port),
		"PGSSLMODE="+values.sslMode(),
		"KPG_TARGET="+t.ID(),
	)
	if t.Provider != "" {
		env = append(env, "KPG_PROVIDER="+t.Provider)
	}
	if values.User != "" {
		env = append(env, "PGUSER="+values.User)
	}
	if values.Password != "" {
		env = append(env, "PGPASSWORD="+values.Password)
	}
	if values.Database != "" {
		env = append(env, "PGDATABASE="+values.Database)
	}
	return env
}
