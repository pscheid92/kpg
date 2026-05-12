package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/pscheid92/kpg/internal/buildinfo"
	"github.com/pscheid92/kpg/internal/kpg"
)

func TestRootHelpShowsShortFlags(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		t.Fatal("kube factory should not be called for help")
		return nil, nil
	})
	cmd.SetArgs([]string{"-h"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, want := range []string{"-c, --context", "-n, --namespace", "-p, --local-port", "-o, --output"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
}

func TestPublicRootCommandHelp(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand(&out, io.Discard)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "Connect to Kubernetes-hosted Postgres databases") || !strings.Contains(got, "connect") {
		t.Fatalf("unexpected help:\n%s", got)
	}
}

func TestRootRejectsInvalidSharedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "output",
			args: []string{"list", "-o", "yaml"},
			want: `invalid --output "yaml"`,
		},
		{
			name: "local-port",
			args: []string{"list", "-p", "70000"},
			want: "invalid --local-port 70000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCommand(io.Discard, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
				t.Fatal("kube factory should not be called")
				return nil, nil
			})
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestConnectParsesShortFlagsAfterTarget(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeKube{
		targets: []kpg.Target{{Namespace: "app", Cluster: "app-db"}},
	}
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		if opts.Context != "prod" || opts.Namespace != "app" || opts.LocalPort != 15432 || opts.Output != "json" || !opts.OutputExplicit {
			t.Fatalf("unexpected opts: %#v", opts)
		}
		return fake, nil
	})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"connect", "app-db", "-c", "prod", "-n", "app", "-p", "15432", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if fake.portForwardCalls != 1 {
		t.Fatalf("portForwardCalls = %d", fake.portForwardCalls)
	}
	if !strings.Contains(out.String(), `"PGPORT": 15432`) {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestEnvAliasStillConnects(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeKube{
		targets: []kpg.Target{{Namespace: "app", Cluster: "app-db"}},
	}
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		return fake, nil
	})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"env", "app-db", "-p", "15432"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if fake.portForwardCalls != 1 {
		t.Fatalf("portForwardCalls = %d", fake.portForwardCalls)
	}
}

func TestListCommandPrintsTargets(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeKube{
		targets: []kpg.Target{
			{Provider: kpg.ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
			{Provider: kpg.ProviderZalando, Namespace: "legacy", Cluster: "acid-main", Database: "app", User: "app_user"},
		},
	}
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		return fake, nil
	})
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"TARGET", "PROVIDER", "app/app-db", "legacy/acid-main", "zalando"} {
		if !strings.Contains(got, want) {
			t.Fatalf("list output missing %q:\n%s", want, got)
		}
	}
}

func TestListCommandPrintsJSON(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeKube{
		targets: []kpg.Target{
			{Provider: kpg.ProviderCNPG, Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
		},
	}
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		if opts.Output != "json" || !opts.OutputExplicit {
			t.Fatalf("unexpected opts: %#v", opts)
		}
		return fake, nil
	})
	cmd.SetArgs([]string{"list", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{`"target": "app/app-db"`, `"provider": "cnpg"`, `"database": "app"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("list json missing %q:\n%s", want, got)
		}
	}
}

func TestVersionCommandPrintsBuildInfo(t *testing.T) {
	restoreBuildInfo := setBuildInfo(t, "1.2.3", "abc123", "2026-05-09T12:00:00Z")
	defer restoreBuildInfo()

	var out bytes.Buffer
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		t.Fatal("kube factory should not be called for version")
		return nil, nil
	})
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"kpg 1.2.3", "commit: abc123", "built: 2026-05-09T12:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("version output missing %q:\n%s", want, got)
		}
	}
}

func TestVersionCommandPrintsJSON(t *testing.T) {
	restoreBuildInfo := setBuildInfo(t, "1.2.3", "abc123", "2026-05-09T12:00:00Z")
	defer restoreBuildInfo()

	var out bytes.Buffer
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		t.Fatal("kube factory should not be called for version")
		return nil, nil
	})
	cmd.SetArgs([]string{"version", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{`"version": "1.2.3"`, `"commit": "abc123"`, `"date": "2026-05-09T12:00:00Z"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("version json missing %q:\n%s", want, got)
		}
	}
}

func TestRootVersionFlag(t *testing.T) {
	restoreBuildInfo := setBuildInfo(t, "1.2.3", "abc123", "2026-05-09T12:00:00Z")
	defer restoreBuildInfo()

	var out bytes.Buffer
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		t.Fatal("kube factory should not be called for --version")
		return nil, nil
	})
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "kpg 1.2.3\n" {
		t.Fatalf("version flag output = %q", got)
	}
}

func TestConnectRejectsOutputWithCommand(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		t.Fatal("kube factory should not be called")
		return nil, nil
	})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"connect", "app-db", "-o", "json", "--", "psql"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--output cannot be combined") {
		t.Fatalf("expected output command conflict, got %v", err)
	}
}

func TestConnectTargetCompletion(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeKube{
		targets: []kpg.Target{
			{Namespace: "app", Cluster: "app-db", Database: "app", User: "app"},
			{Namespace: "billing", Cluster: "billing-db", Database: "billing", User: "billing"},
			{Namespace: "identity", Cluster: "identity-db", Database: "identity", User: "identity"},
		},
	}
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		return fake, nil
	})
	cmd.SetArgs([]string{"__complete", "connect", ""})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"app/app-db\tdatabase=app user=app",
		"billing/billing-db\tdatabase=billing user=billing",
		"identity/identity-db\tdatabase=identity user=identity",
		":4",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("completion missing %q:\n%s", want, got)
		}
	}
}

func TestSplitConnectArgsAtDash(t *testing.T) {
	target, clientArgs, err := splitConnectArgsAtDash(0, []string{"psql", "-c", "select 1"})
	if err != nil {
		t.Fatal(err)
	}
	if target != "" || strings.Join(clientArgs, " ") != "psql -c select 1" {
		t.Fatalf("no-target command split = target %q args %#v", target, clientArgs)
	}

	target, clientArgs, err = splitConnectArgsAtDash(1, []string{"app-db", "psql"})
	if err != nil {
		t.Fatal(err)
	}
	if target != "app-db" || strings.Join(clientArgs, " ") != "psql" {
		t.Fatalf("target command split = target %q args %#v", target, clientArgs)
	}

	target, clientArgs, err = splitConnectArgsAtDash(-1, []string{"app-db"})
	if err != nil {
		t.Fatal(err)
	}
	if target != "app-db" || clientArgs != nil {
		t.Fatalf("target-only split = target %q args %#v", target, clientArgs)
	}
}

func TestSplitConnectArgsRejectsAccidentalExtraArgs(t *testing.T) {
	_, _, err := splitConnectArgsAtDash(-1, []string{"app-db", "psql"})
	if err == nil || !strings.Contains(err.Error(), "without --") {
		t.Fatalf("expected missing dash error, got %v", err)
	}
	_, _, err = splitConnectArgsAtDash(2, []string{"app-db", "extra", "psql"})
	if err == nil || !strings.Contains(err.Error(), "before --") {
		t.Fatalf("expected before dash error, got %v", err)
	}
}

func TestConnectTargetCompletionHonorsNamespace(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeKube{
		targets: []kpg.Target{
			{Namespace: "app", Cluster: "app-db"},
			{Namespace: "billing", Cluster: "billing-db"},
		},
	}
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		if opts.Namespace != "app" {
			t.Fatalf("namespace = %q", opts.Namespace)
		}
		return fake, nil
	})
	cmd.SetArgs([]string{"__complete", "connect", "-n", "app", ""})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "app/app-db") {
		t.Fatalf("completion missing app target:\n%s", got)
	}
	if strings.Contains(got, "billing/billing-db") {
		t.Fatalf("completion should not include billing target:\n%s", got)
	}
}

func TestLastRunsCommandAfterDash(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	if err := kpg.WriteLastTarget(kpg.LastTarget{Namespace: "app", Cluster: "app-db"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	fake := &fakeKube{
		targets: []kpg.Target{{Namespace: "app", Cluster: "app-db"}},
	}
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		return fake, nil
	})
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"last", "--", "sh", "-c", "printf last"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "last" {
		t.Fatalf("stdout = %q", out.String())
	}
	if fake.portForwardCalls != 1 {
		t.Fatalf("portForwardCalls = %d", fake.portForwardCalls)
	}
}

func TestLastRejectsArgsWithoutDash(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCommand(&out, io.Discard, func(opts kpg.Options) (kpg.Kube, error) {
		t.Fatal("kube factory should not be called")
		return nil, nil
	})
	cmd.SetArgs([]string{"last", "psql"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "without --") {
		t.Fatalf("expected last args error, got %v", err)
	}
}

func TestContextFlagCompletion(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCommandWithCompleters(
		&out,
		io.Discard,
		func(opts kpg.Options) (kpg.Kube, error) {
			t.Fatal("kube factory should not be called for context completion")
			return nil, nil
		},
		func() ([]string, error) {
			return []string{"dev", "prod", "staging"}, nil
		},
		func(context.Context, kpg.Options) ([]string, error) {
			t.Fatal("namespace lister should not be called for context completion")
			return nil, nil
		},
	)
	cmd.SetArgs([]string{"__complete", "-c", "pr"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "prod") || strings.Contains(got, "dev") || strings.Contains(got, "staging") {
		t.Fatalf("unexpected context completion:\n%s", got)
	}
}

func TestNamespaceFlagCompletionUsesSelectedContext(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCommandWithCompleters(
		&out,
		io.Discard,
		func(opts kpg.Options) (kpg.Kube, error) {
			t.Fatal("kube factory should not be called for namespace completion")
			return nil, nil
		},
		func() ([]string, error) { return nil, nil },
		func(_ context.Context, opts kpg.Options) ([]string, error) {
			if opts.Context != "prod" {
				t.Fatalf("context = %q", opts.Context)
			}
			return []string{"app", "billing", "identity"}, nil
		},
	)
	cmd.SetArgs([]string{"__complete", "-c", "prod", "-n", "app"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "app") || strings.Contains(got, "billing") || strings.Contains(got, "identity") {
		t.Fatalf("unexpected namespace completion:\n%s", got)
	}
}

type fakeKube struct {
	targets          []kpg.Target
	portForwardCalls int
}

func (f *fakeKube) ListTargets(_ context.Context, opts kpg.Options) ([]kpg.Target, error) {
	targets := make([]kpg.Target, 0, len(f.targets))
	for _, target := range f.targets {
		if opts.Namespace != "" && target.Namespace != opts.Namespace {
			continue
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (f *fakeKube) ListClusterUsers(context.Context, kpg.Target) ([]string, error) {
	return nil, nil
}

func (f *fakeKube) ReadCredentials(context.Context, kpg.Options, kpg.Target) (kpg.AppSecret, bool, error) {
	return kpg.AppSecret{}, false, nil
}

func (f *fakeKube) PortForward(ctx context.Context, _ kpg.Options, _ kpg.Target, _ int, _ io.Writer, _ io.Writer, readyCh chan struct{}) error {
	f.portForwardCalls++
	if readyCh != nil {
		close(readyCh)
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func setBuildInfo(t *testing.T, version, commit, date string) func() {
	t.Helper()
	oldVersion := buildinfo.Version
	oldCommit := buildinfo.Commit
	oldDate := buildinfo.Date
	buildinfo.Version = version
	buildinfo.Commit = commit
	buildinfo.Date = date
	return func() {
		buildinfo.Version = oldVersion
		buildinfo.Commit = oldCommit
		buildinfo.Date = oldDate
	}
}
