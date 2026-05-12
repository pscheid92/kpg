package kpg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func Connect(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options, targetText string, clientArgs []string, storeLast bool) error {
	if len(clientArgs) > 0 && opts.OutputExplicit {
		return fmt.Errorf("--output cannot be combined with a command after --")
	}

	t, secret, localPort, err := prepareConnection(ctx, kube, opts, targetText)
	if err != nil {
		return err
	}

	values := EnvValues{
		Host:     "127.0.0.1",
		Port:     localPort,
		User:     t.User,
		Password: secret.Password,
		Database: t.Database,
		SSLMode:  DefaultSSLMode,
	}
	switch {
	case len(clientArgs) > 0:
		return connectExec(ctx, stdout, stderr, kube, opts, t, values, clientArgs, storeLast)
	case opts.Selection.Enabled && !opts.OutputExplicit:
		return connectShell(ctx, stdout, stderr, kube, opts, t, values, storeLast)
	default:
		return connectRender(ctx, stdout, stderr, kube, opts, t, values, storeLast)
	}
}

func prepareConnection(ctx context.Context, kube Kube, opts Options, targetText string) (Target, AppSecret, int, error) {
	t, err := resolveConnectTarget(ctx, kube, opts, targetText)
	if err != nil {
		return Target{}, AppSecret{}, 0, err
	}

	if enriched, err := kube.EnrichTarget(ctx, t); err == nil {
		t = enriched
	}

	if err := disambiguateConnectionChoices(&opts, t); err != nil {
		return Target{}, AppSecret{}, 0, err
	}

	t, secret, err := kube.ResolveConnection(ctx, opts, t)
	if err != nil {
		return Target{}, AppSecret{}, 0, fmt.Errorf("secret lookup failed: %w", err)
	}

	localPort := opts.LocalPort
	if localPort == 0 {
		localPort, err = FreeLocalPort()
		if err != nil {
			return Target{}, AppSecret{}, 0, fmt.Errorf("could not choose local port: %w", err)
		}
	} else if err := EnsurePortFree(localPort); err != nil {
		return Target{}, AppSecret{}, 0, fmt.Errorf("local port %d is unavailable: %w", localPort, err)
	}
	return t, secret, localPort, nil
}

func resolveConnectTarget(ctx context.Context, kube Kube, opts Options, targetText string) (Target, error) {
	if opts.Namespace == "" {
		opts.Namespace = explicitTargetNamespace(targetText)
	}
	targets, err := kube.ListTargets(ctx, opts)
	if err != nil {
		return Target{}, fmt.Errorf("discovery failed: %w", err)
	}
	if targetText != "" {
		return ResolveTarget(targetText, targets)
	}
	if !opts.Selection.Enabled {
		return Target{}, fmt.Errorf("missing target\nusage: kpg connect [flags] <cluster|namespace/cluster|substring>\ntry: kpg list")
	}
	if opts.Selection.Interactive {
		return PickTargetInteractive(opts.Selection.In, opts.Selection.Out, targets)
	}
	return PickTarget(opts.Selection.In, opts.Selection.Out, targets)
}

func disambiguateConnectionChoices(opts *Options, t Target) error {
	if !opts.Selection.Enabled {
		return nil
	}
	pick := func(label string, options []string) (string, error) {
		if opts.Selection.Interactive {
			return PickFromListInteractive(opts.Selection.In, opts.Selection.Out, label, options)
		}
		return PickFromList(opts.Selection.In, opts.Selection.Out, label, options)
	}
	if opts.Database == "" && len(t.DatabaseOptions) > 1 {
		choice, err := pick("database", t.DatabaseOptions)
		if err != nil {
			return err
		}
		opts.Database = choice
	}
	if opts.User == "" && opts.Database != "" {
		if owner := t.DatabaseOwners[opts.Database]; owner != "" {
			opts.User = owner
		}
	}
	if opts.User == "" && len(t.UserOptions) > 1 {
		choice, err := pick("user", t.UserOptions)
		if err != nil {
			return err
		}
		opts.User = choice
	}
	return nil
}

func explicitTargetNamespace(input string) string {
	_, targetText := splitProviderPrefix(strings.TrimSpace(input))
	namespace, cluster, found := strings.Cut(targetText, "/")
	if !found || namespace == "" || cluster == "" {
		return ""
	}
	return namespace
}

func connectRender(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options, t Target, values EnvValues, storeLast bool) error {
	if err := RenderEnv(stdout, opts.Output, values); err != nil {
		return fmt.Errorf("render failed: %w", err)
	}
	_, _ = fmt.Fprintf(stderr, "port-forwarding %s/%s to 127.0.0.1:%d; press Ctrl-C to stop\n", t.Namespace, serviceName(t), values.Port)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := kube.PortForward(ctx, opts, t, values.Port, stdout, stderr, nil); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("port-forward failed: %w", err)
	}
	storeLastTarget(stderr, t, storeLast)
	return nil
}

func connectShell(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options, t Target, values EnvValues, storeLast bool) error {
	shell, err := resolveShell()
	if err != nil {
		return err
	}
	runErr := withPortForward(ctx, stdout, stderr, kube, opts, t, values.Port, func(ctx context.Context) error {
		_, _ = fmt.Fprintf(stderr, "connected to %s on %s:%d\n", t.ID(), values.Host, values.Port)
		_, _ = fmt.Fprintf(stderr, "starting subshell %s with PG* variables exported; exit subshell to disconnect\n", filepath.Base(shell))
		return runChild(ctx, []string{shell}, t, values, stdout, stderr)
	})
	storeLastTarget(stderr, t, storeLast && runErr == nil)
	return runErr
}

func connectExec(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options, t Target, values EnvValues, clientArgs []string, storeLast bool) error {
	_, _ = fmt.Fprintf(stderr, "port-forwarding %s/%s to 127.0.0.1:%d; press Ctrl-C to stop\n", t.Namespace, serviceName(t), values.Port)
	runErr := withPortForward(ctx, stdout, stderr, kube, opts, t, values.Port, func(ctx context.Context) error {
		return runClient(ctx, clientArgs, t, values, stdout, stderr)
	})
	storeLastTarget(stderr, t, storeLast && runErr == nil)
	return runErr
}

func withPortForward(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options, t Target, localPort int, run func(context.Context) error) error {
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	pfCtx, stopPortForward := context.WithCancel(ctx)
	defer stopPortForward()

	readyCh := make(chan struct{})
	portForwardErr := make(chan error, 1)
	go func() {
		portForwardErr <- kube.PortForward(pfCtx, opts, t, localPort, stdout, stderr, readyCh)
	}()

	select {
	case <-readyCh:
	case err := <-portForwardErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("port-forward failed: %w", err)
		}
		return fmt.Errorf("port-forward stopped before it was ready")
	case <-ctx.Done():
		return ctx.Err()
	}

	runErr := run(ctx)
	stopPortForward()
	if err := <-portForwardErr; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("port-forward failed: %w", err)
	}
	return runErr
}

func storeLastTarget(stderr io.Writer, t Target, enabled bool) {
	if !enabled {
		return
	}
	if err := WriteLastTarget(LastTarget{Provider: t.Provider, Namespace: t.Namespace, Cluster: t.Cluster}); err != nil {
		_, _ = fmt.Fprintf(stderr, "warning: could not write last target: %v\n", err)
	}
}

func serviceName(t Target) string {
	if t.ServiceName != "" {
		return t.ServiceName
	}
	return t.Cluster + "-rw"
}
