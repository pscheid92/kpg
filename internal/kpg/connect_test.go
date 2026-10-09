package kpg

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConnectMissingAppSecretUsesBootstrapFallbackAndStoresNoSecrets(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	k := &fakeKube{
		targets: []Target{
			{Namespace: "app", Cluster: "app-db", Database: "bootstrapdb", User: "owner"},
		},
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := Connect(context.Background(), &out, &errOut, k, Options{}, "app-db", nil, true); err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "export PGUSER=owner\n") || !strings.Contains(out.String(), "export PGDATABASE=bootstrapdb\n") {
		t.Fatalf("unexpected env:\n%s", out.String())
	}
	data, err := os.ReadFile(filepath.Join(stateHome, "kpg", StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "owner") || strings.Contains(string(data), "bootstrapdb") {
		t.Fatalf("last target leaked connection data: %s", string(data))
	}
	if k.portForwardCalls != 1 {
		t.Fatalf("portForwardCalls = %d", k.portForwardCalls)
	}
}

func TestConnectNoTargetNonTTYShowsUsage(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err := Connect(context.Background(), io.Discard, io.Discard, k, Options{}, "", nil, false)
	if err == nil || !strings.Contains(err.Error(), "missing target") || !strings.Contains(err.Error(), "try: kpg list") {
		t.Fatalf("expected missing target usage, got %v", err)
	}
}

func TestResolveConnectTargetRestrictsDiscoveryToExplicitTargetNamespace(t *testing.T) {
	k := &fakeKube{
		targets: []Target{
			{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db"},
			{Provider: ProviderCNPG, Namespace: "billing", Cluster: "billing-db"},
		},
	}
	got, err := resolveConnectTarget(context.Background(), k, Options{}, "cnpg:app/app-db")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "app/app-db" {
		t.Fatalf("target = %s", got.ID())
	}
	if len(k.listOptions) != 1 || k.listOptions[0].Namespace != "app" {
		t.Fatalf("discovery namespace = %#v", k.listOptions)
	}
}

func TestResolveConnectTargetKeepsNamespaceFlag(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	_, err := resolveConnectTarget(context.Background(), k, Options{Namespace: "override"}, "app/app-db")
	if err != nil {
		t.Fatal(err)
	}
	if len(k.listOptions) != 1 || k.listOptions[0].Namespace != "override" {
		t.Fatalf("discovery namespace = %#v", k.listOptions)
	}
}

func TestConnectNoTargetPickerSelectsTarget(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	k := &fakeKube{
		targets: []Target{
			{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
			{Provider: ProviderCNPG, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		},
	}
	var prompt bytes.Buffer
	var out bytes.Buffer
	var errOut bytes.Buffer
	opts := Options{
		OutputExplicit: true,
		Selection: Selection{
			In:  strings.NewReader("2\n"),
			Out: &prompt,
		},
	}
	err := Connect(context.Background(), &out, &errOut, k, opts, "", nil, true)
	if err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	if !strings.Contains(prompt.String(), "1  app/app-db") || !strings.Contains(prompt.String(), "2  billing/billing-db") {
		t.Fatalf("unexpected prompt:\n%s", prompt.String())
	}
	if !strings.Contains(out.String(), "export PGDATABASE=billing\n") {
		t.Fatalf("selected target env mismatch:\n%s", out.String())
	}
	last, err := ReadLastTarget()
	if err != nil {
		t.Fatal(err)
	}
	if last.Namespace != "billing" || last.Cluster != "billing-db" {
		t.Fatalf("last = %#v", last)
	}
}

func TestConnectNoTargetPickerStartsShell(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	shellPath := filepath.Join(t.TempDir(), "kpg-test-shell")
	if err := os.WriteFile(shellPath, []byte("#!/bin/sh\nprintf '%s|%s|%s|%s|%s|%s|%s' \"$KPG_TARGET\" \"$KPG_PROVIDER\" \"$PGHOST\" \"$PGPORT\" \"$PGUSER\" \"$PGDATABASE\" \"$PGSSLMODE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shellPath)

	k := &fakeKube{
		targets: []Target{
			{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
			{Provider: ProviderCNPG, Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
		},
	}
	var prompt bytes.Buffer
	var out bytes.Buffer
	var errOut bytes.Buffer
	opts := Options{
		Selection: Selection{
			In:  strings.NewReader("2\n"),
			Out: &prompt,
		},
	}
	err := Connect(context.Background(), &out, &errOut, k, opts, "", nil, true)
	if err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	parts := strings.Split(out.String(), "|")
	if len(parts) != 7 {
		t.Fatalf("unexpected shell output: %q", out.String())
	}
	if parts[0] != "billing/billing-db" || parts[1] != ProviderCNPG || parts[2] != "127.0.0.1" || parts[4] != "billing" || parts[5] != "billing" || parts[6] != "disable" {
		t.Fatalf("unexpected shell env: %q", out.String())
	}
	if parts[3] == "" {
		t.Fatalf("PGPORT was empty: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "starting subshell") || !strings.Contains(errOut.String(), "exit subshell to disconnect") {
		t.Fatalf("missing shell status:\n%s", errOut.String())
	}
	last, err := ReadLastTarget()
	if err != nil {
		t.Fatal(err)
	}
	if last.Namespace != "billing" || last.Cluster != "billing-db" {
		t.Fatalf("last = %#v", last)
	}
}

func TestConnectUserAndDatabaseFlagsOverrideTargetValues(t *testing.T) {
	k := &fakeKube{
		targets: []Target{
			{
				Provider:  ProviderZalando,
				Namespace: "legacy",
				Cluster:   "acid-main",
				Database:  "app",
				User:      "default_owner",
			},
		},
		secrets: map[string]AppSecret{
			"legacy/acid-main": {Password: "rpw"},
		},
	}
	var out bytes.Buffer
	opts := Options{
		User:           "reporting_user",
		Database:       "reports",
		OutputExplicit: true,
	}
	if err := Connect(context.Background(), &out, io.Discard, k, opts, "acid-main", nil, false); err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	if !strings.Contains(out.String(), "export PGUSER=reporting_user\n") || !strings.Contains(out.String(), "export PGDATABASE=reports\n") || !strings.Contains(out.String(), "export PGPASSWORD=rpw\n") {
		t.Fatalf("unexpected env:\n%s", out.String())
	}
}

func TestConnectUsesSelectedDatabaseOwnerWhenUserIsNotExplicit(t *testing.T) {
	k := &fakeKube{
		targets: []Target{
			{
				Provider:        ProviderZalando,
				Namespace:       "legacy",
				Cluster:         "acid-main",
				Database:        "app",
				User:            "default_owner",
				DatabaseOptions: []string{"app", "reports"},
				UserOptions:     []string{"default_owner", "reporting_user"},
				DatabaseOwners:  map[string]string{"reports": "reporting_user"},
			},
		},
	}
	var prompt bytes.Buffer
	var out bytes.Buffer
	opts := Options{
		OutputExplicit: true,
		Selection: Selection{
			In:  strings.NewReader("2\n"),
			Out: &prompt,
		},
	}
	if err := Connect(context.Background(), &out, io.Discard, k, opts, "acid-main", nil, false); err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	if !strings.Contains(prompt.String(), "Select database:") || strings.Contains(prompt.String(), "Select user:") {
		t.Fatalf("unexpected prompts:\n%s", prompt.String())
	}
	if !strings.Contains(out.String(), "export PGUSER=reporting_user\n") || !strings.Contains(out.String(), "export PGDATABASE=reports\n") {
		t.Fatalf("database owner not applied:\n%s", out.String())
	}
}

func TestConnectPromptsForAmbiguousUserAndDatabase(t *testing.T) {
	k := &fakeKube{
		targets: []Target{
			{
				Provider:        ProviderZalando,
				Namespace:       "legacy",
				Cluster:         "acid-main",
				Database:        "app",
				User:            "default_owner",
				ServiceName:     "acid-main",
				SecretName:      "default_owner.acid-main.credentials.postgresql.acid.zalan.do",
				SecretNamespace: "legacy",
				DatabaseOptions: []string{"app", "reports"},
				UserOptions:     []string{"default_owner", "reporting_user"},
			},
		},
	}
	var prompt bytes.Buffer
	var out bytes.Buffer
	opts := Options{
		OutputExplicit: true,
		Selection: Selection{
			In:  strings.NewReader("2\n2\n"),
			Out: &prompt,
		},
	}
	if err := Connect(context.Background(), &out, io.Discard, k, opts, "acid-main", nil, false); err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	if !strings.Contains(prompt.String(), "Select database:") || !strings.Contains(prompt.String(), "Select user:") {
		t.Fatalf("missing prompts:\n%s", prompt.String())
	}
	if !strings.Contains(out.String(), "export PGUSER=reporting_user\n") || !strings.Contains(out.String(), "export PGDATABASE=reports\n") {
		t.Fatalf("interactive picks not applied:\n%s", out.String())
	}
}

func TestResolveShellFallback(t *testing.T) {
	t.Setenv("SHELL", "")
	got, err := resolveShell()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/bin/sh" {
		t.Fatalf("shell = %q", got)
	}
}

func TestResolveShellRejectsMissingShell(t *testing.T) {
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	_, err := resolveShell()
	if err == nil || !strings.Contains(err.Error(), "could not start shell") {
		t.Fatalf("expected missing shell error, got %v", err)
	}
}

func TestResolveShellRejectsDirectory(t *testing.T) {
	t.Setenv("SHELL", t.TempDir())
	_, err := resolveShell()
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory shell error, got %v", err)
	}
}

func TestResolveShellRejectsNonExecutable(t *testing.T) {
	shellPath := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shellPath, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shellPath)
	_, err := resolveShell()
	if err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("expected non-executable shell error, got %v", err)
	}
}

func TestConnectLocalPortConflictDoesNotPortForward(t *testing.T) {
	port, err := FreeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = ln.Close()
	}()

	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err = Connect(context.Background(), io.Discard, io.Discard, k, Options{LocalPort: port}, "app-db", nil, false)
	if err == nil {
		t.Fatal("expected local port conflict")
	}
	if k.portForwardCalls != 0 {
		t.Fatalf("portForwardCalls = %d", k.portForwardCalls)
	}
}

func TestConnectExecInjectsPGEnvironmentAndStoresLastTarget(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	k := &fakeKube{
		targets: []Target{
			{Namespace: "app", Cluster: "app-db", Database: "bootstrapdb", User: "owner"},
		},
		secrets: map[string]AppSecret{
			"app/app-db": {Username: "appuser", Password: "secret", Database: "appdb"},
		},
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	clientArgs := []string{"sh", "-c", "printf '%s|%s|%s|%s|%s|%s' \"$PGHOST\" \"$PGPORT\" \"$PGUSER\" \"$PGPASSWORD\" \"$PGDATABASE\" \"$PGSSLMODE\""}
	err := Connect(context.Background(), &out, &errOut, k, Options{}, "app-db", clientArgs, true)
	if err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	parts := strings.Split(out.String(), "|")
	if len(parts) != 6 {
		t.Fatalf("unexpected client output: %q", out.String())
	}
	if parts[0] != "127.0.0.1" || parts[2] != "appuser" || parts[3] != "secret" || parts[4] != "appdb" || parts[5] != "disable" {
		t.Fatalf("unexpected env output: %q", out.String())
	}
	if parts[1] == "" {
		t.Fatalf("PGPORT was empty: %q", out.String())
	}
	data, err := os.ReadFile(filepath.Join(stateHome, "kpg", StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "appuser") {
		t.Fatalf("last target leaked secret data: %s", string(data))
	}
}

func TestConnectExecReturnsClientExitCode(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err := Connect(context.Background(), io.Discard, io.Discard, k, Options{}, "app-db", []string{"sh", "-c", "exit 7"}, false)
	var exitErr ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d", exitErr.ExitCode())
	}
	if got, want := exitErr.Error(), "command exited with code 7"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func shortReconnectDelay(t *testing.T) {
	t.Helper()
	old := reconnectDelay
	reconnectDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() { reconnectDelay = old })
}

func TestConnectStoresLastTargetOnceTunnelIsReadyEvenIfCommandFails(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	k := &fakeKube{
		targets: []Target{{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db"}},
	}
	err := Connect(context.Background(), io.Discard, io.Discard, k, Options{}, "app-db", []string{"sh", "-c", "exit 3"}, true)
	var exitErr ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 3 {
		t.Fatalf("expected exit code 3, got %v", err)
	}
	last, err := ReadLastTarget()
	if err != nil {
		t.Fatalf("last target should be stored once the tunnel worked: %v", err)
	}
	if last.Provider != ProviderCNPG || last.Namespace != "app" || last.Cluster != "app-db" {
		t.Fatalf("last = %#v", last)
	}
}

func TestConnectDoesNotStoreLastTargetWhenTunnelNeverReady(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
		portForwardScript: []func(ready chan struct{}) error{
			func(chan struct{}) error { return errors.New("pod not found") },
		},
	}
	var errOut bytes.Buffer
	err := Connect(context.Background(), io.Discard, &errOut, k, Options{}, "app-db", []string{"sh", "-c", "exit 0"}, true)
	if err == nil || !strings.Contains(err.Error(), "port-forward failed") || !strings.Contains(err.Error(), "pod not found") {
		t.Fatalf("expected port-forward failure, got %v", err)
	}
	if _, err := ReadLastTarget(); err == nil {
		t.Fatal("last target must not be stored when the tunnel never came up")
	}
	if k.portForwardCalls != 1 {
		t.Fatalf("portForwardCalls = %d, want no reconnect before readiness", k.portForwardCalls)
	}
}

func TestConnectReconnectsWhenTunnelDrops(t *testing.T) {
	shortReconnectDelay(t)
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
		portForwardScript: []func(ready chan struct{}) error{
			func(ready chan struct{}) error {
				close(ready)
				return errors.New("lost connection to pod")
			},
		},
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	err := Connect(context.Background(), &out, &errOut, k, Options{}, "app-db", []string{"sh", "-c", "sleep 0.2; printf ok"}, false)
	if err != nil {
		t.Fatalf("Connect error = %v, stderr = %s", err, errOut.String())
	}
	if out.String() != "ok" {
		t.Fatalf("command output = %q", out.String())
	}
	if k.portForwardCalls != 2 {
		t.Fatalf("portForwardCalls = %d, want 2", k.portForwardCalls)
	}
	for _, want := range []string{"lost: lost connection to pod", "reconnecting", "attempt 1 of 5", "re-established"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, errOut.String())
		}
	}
}

func TestConnectReportsTunnelLossAfterReconnectAttemptsFail(t *testing.T) {
	shortReconnectDelay(t)
	script := []func(ready chan struct{}) error{
		func(ready chan struct{}) error {
			close(ready)
			return errors.New("lost connection to pod")
		},
	}
	for range maxReconnectAttempts {
		script = append(script, func(chan struct{}) error { return errors.New("pod gone") })
	}
	k := &fakeKube{
		targets:           []Target{{Namespace: "app", Cluster: "app-db"}},
		portForwardScript: script,
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	err := Connect(context.Background(), &out, &errOut, k, Options{OutputExplicit: true}, "app-db", nil, false)
	if err == nil || !strings.Contains(err.Error(), "port-forward failed") || !strings.Contains(err.Error(), "pod gone") {
		t.Fatalf("expected tunnel loss error, got %v", err)
	}
	if !strings.Contains(out.String(), "export PGHOST=127.0.0.1\n") {
		t.Fatalf("values should have been printed once the tunnel was ready:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "giving up after 5 reconnect attempts") {
		t.Fatalf("stderr missing give-up message:\n%s", errOut.String())
	}
	if k.portForwardCalls != 1+maxReconnectAttempts {
		t.Fatalf("portForwardCalls = %d", k.portForwardCalls)
	}
}

func TestConnectWarnsWhenNoCredentialsFound(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db", User: "owner", SecretName: "app-db-app"}},
	}
	var errOut bytes.Buffer
	if err := Connect(context.Background(), io.Discard, &errOut, k, Options{OutputExplicit: true}, "app-db", nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "warning: no credentials found for user owner in secret app/app-db-app") {
		t.Fatalf("missing credentials warning:\n%s", errOut.String())
	}
}

func TestChildEnvScrubsConflictingVariables(t *testing.T) {
	env := childEnv(
		[]string{"HOME=/home/x", "PGHOSTADDR=10.0.0.1", "PGSERVICE=prod", "PGSERVICEFILE=/svc", "PGUSER=old", "PGPASSWORD=old", "KPG_TARGET=old/old"},
		Target{Provider: ProviderCNPG, Namespace: "app", Cluster: "app-db"},
		EnvValues{Host: "127.0.0.1", Port: 15432, User: "app", Password: "pw", Database: "appdb"},
	)
	joined := strings.Join(env, "\n") + "\n"
	for _, absent := range []string{"PGHOSTADDR=", "PGSERVICE=", "PGSERVICEFILE=", "PGUSER=old", "PGPASSWORD=old", "KPG_TARGET=old"} {
		if strings.Contains(joined, absent) {
			t.Fatalf("child env still contains %q:\n%s", absent, joined)
		}
	}
	for _, want := range []string{"HOME=/home/x\n", "PGHOST=127.0.0.1\n", "PGPORT=15432\n", "PGUSER=app\n", "PGPASSWORD=pw\n", "PGDATABASE=appdb\n", "PGSSLMODE=disable\n", "KPG_TARGET=app/app-db\n", "KPG_PROVIDER=cnpg\n"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("child env missing %q:\n%s", want, joined)
		}
	}
	if strings.Count(joined, "PGUSER=") != 1 {
		t.Fatalf("PGUSER must appear once:\n%s", joined)
	}
}

func TestLookupShellReturnsFirstAvailableCandidate(t *testing.T) {
	got, err := lookupShell("", "definitely-missing-shell-kpg", "sh")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "sh" {
		t.Fatalf("shell = %q", got)
	}
	if _, err := lookupShell("definitely-missing-shell-kpg"); err == nil || !strings.Contains(err.Error(), "could not find a shell") {
		t.Fatalf("expected missing shell error, got %v", err)
	}
}

func TestConnectRejectsOutputWithCommand(t *testing.T) {
	k := &fakeKube{
		targets: []Target{{Namespace: "app", Cluster: "app-db"}},
	}
	err := Connect(context.Background(), io.Discard, io.Discard, k, Options{OutputExplicit: true}, "app-db", []string{"psql"}, false)
	if err == nil || !strings.Contains(err.Error(), "--output cannot be combined") {
		t.Fatalf("expected output command conflict, got %v", err)
	}
	if k.portForwardCalls != 0 {
		t.Fatalf("portForwardCalls = %d", k.portForwardCalls)
	}
}
