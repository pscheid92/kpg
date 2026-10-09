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
	"sync"
	"syscall"
	"time"
)

// Connect resolves a target, opens the tunnel, and then prints the connection
// values, starts a subshell, or runs clientArgs with the PG* values exported.
// kpg's own messages go to stderr; a child process inherits stdout and stderr
// unchanged so that interactive clients still see a terminal.
func Connect(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options, targetText string, clientArgs []string, storeLast bool) error {
	if len(clientArgs) > 0 && opts.OutputExplicit {
		return errors.New("--output cannot be combined with a command after --")
	}
	t, secret, localPort, err := prepareConnection(ctx, kube, opts, targetText)
	if err != nil {
		return err
	}
	status := &lockedWriter{w: stderr}
	childStderr := stderr
	if _, isFile := stderr.(*os.File); !isFile {
		// os/exec copies into non-file writers from its own goroutine, so the
		// child's stderr has to share the lock with kpg's status messages.
		childStderr = status
	}
	if secret.Password == "" {
		_, _ = fmt.Fprintf(status, "warning: no credentials found for user %s in secret %s/%s; PGPASSWORD is not set and the client will prompt\n", valueOrDash(t.User), secretNamespace(t), secretName(t))
	}
	session := &tunnelSession{
		stdout: stdout,
		stderr: childStderr,
		status: status,
		kube:   kube,
		opts:   opts,
		target: t,
		values: EnvValues{
			Host:     "127.0.0.1",
			Port:     localPort,
			User:     t.User,
			Password: secret.Password,
			Database: t.Database,
			SSLMode:  DefaultSSLMode,
		},
		storeLast: storeLast,
	}
	switch {
	case len(clientArgs) > 0:
		return session.exec(ctx, clientArgs)
	case opts.Selection.canPrompt() && !opts.OutputExplicit:
		return session.shell(ctx)
	default:
		return session.render(ctx)
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
	if !opts.Selection.canPrompt() {
		return Target{}, errors.New("missing target\nusage: kpg connect [flags] <cluster|namespace/cluster|substring>\ntry: kpg list")
	}
	if opts.Selection.Interactive {
		return PickTargetInteractive(opts.Selection.In, opts.Selection.Out, targets)
	}
	return PickTarget(opts.Selection.In, opts.Selection.Out, targets)
}

func disambiguateConnectionChoices(opts *Options, t Target) error {
	if !opts.Selection.canPrompt() {
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

// tunnelSession is one connection: the resolved target, the values to
// export, and the streams to use. stderr is what the child process inherits;
// status carries kpg's own messages and is safe for concurrent writers.
type tunnelSession struct {
	stdout    io.Writer
	stderr    io.Writer
	status    io.Writer
	kube      Kube
	opts      Options
	target    Target
	values    EnvValues
	storeLast bool
}

// tunnelRun is the work done while the tunnel is open. ctx ends when the
// tunnel is lost for good; sigs delivers Ctrl-C and SIGTERM.
type tunnelRun func(ctx context.Context, sigs <-chan os.Signal) error

func (s *tunnelSession) render(ctx context.Context) error {
	return s.withPortForward(ctx, func(ctx context.Context, sigs <-chan os.Signal) error {
		if err := RenderEnv(s.stdout, s.opts.Output, s.values); err != nil {
			return fmt.Errorf("render failed: %w", err)
		}
		_, _ = fmt.Fprintf(s.status, "port-forwarding %s/%s to 127.0.0.1:%d; press Ctrl-C to stop\n", s.target.Namespace, serviceName(s.target), s.values.Port)
		select {
		case <-sigs:
		case <-ctx.Done():
		}
		return nil
	})
}

func (s *tunnelSession) shell(ctx context.Context) error {
	shell, err := resolveShell()
	if err != nil {
		return err
	}
	return s.withPortForward(ctx, func(_ context.Context, sigs <-chan os.Signal) error {
		_, _ = fmt.Fprintf(s.status, "connected to %s on %s:%d\n", s.target.ID(), s.values.Host, s.values.Port)
		_, _ = fmt.Fprintf(s.status, "starting subshell %s with PG* variables exported; exit subshell to disconnect\n", filepath.Base(shell))
		return runChild(sigs, []string{shell}, s.target, s.values, s.stdout, s.stderr)
	})
}

func (s *tunnelSession) exec(ctx context.Context, clientArgs []string) error {
	return s.withPortForward(ctx, func(_ context.Context, sigs <-chan os.Signal) error {
		_, _ = fmt.Fprintf(s.status, "port-forwarding %s/%s to 127.0.0.1:%d; the tunnel closes when the command exits\n", s.target.Namespace, serviceName(s.target), s.values.Port)
		return runChild(sigs, clientArgs, s.target, s.values, s.stdout, s.stderr)
	})
}

// withPortForward opens the tunnel, waits until it accepts connections, and
// hands control to run. The target is remembered as soon as the tunnel is
// usable, because that is the moment it is known to work regardless of how
// the client exits later. A tunnel that drops while run is active is
// re-established a few times; if that fails, the context given to run is
// cancelled with the cause and the error is reported once run returns.
func (s *tunnelSession) withPortForward(ctx context.Context, run tunnelRun) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	pfCtx, stopPortForward := context.WithCancel(ctx)
	defer stopPortForward()
	readyCh := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.forwardWithRetry(pfCtx, readyCh)
	}()

	select {
	case <-readyCh:
	case err := <-done:
		if !isClosed(readyCh) {
			return portForwardSetupError(err)
		}
		done <- err
	case <-sigs:
		stopPortForward()
		<-done
		return ExitError{Code: exitCodeInterrupted}
	case <-ctx.Done():
		stopPortForward()
		<-done
		return ctx.Err()
	}
	storeLastTarget(s.status, s.target, s.storeLast)

	tunnelCtx, tunnelEnded := context.WithCancelCause(ctx)
	defer tunnelEnded(nil)
	result := make(chan error, 1)
	go func() {
		err := <-done
		tunnelEnded(tunnelCause(err))
		result <- err
	}()
	runErr := run(tunnelCtx, sigs)
	stopPortForward()
	if err := <-result; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("port-forward failed: %w", err)
	}
	return runErr
}

func portForwardSetupError(err error) error {
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("port-forward failed: %w", err)
	}
	return errors.New("port-forward stopped before it was ready")
}

func tunnelCause(err error) error {
	if err == nil {
		return errors.New("port-forward ended")
	}
	return err
}

const maxReconnectAttempts = 5

// reconnectDelay is the pause before reconnect attempt n. Tests shorten it.
var reconnectDelay = func(attempt int) time.Duration {
	return time.Duration(attempt) * time.Second
}

// forwardWithRetry keeps the tunnel up for the session. A tunnel that drops
// after it was ready, for example because the primary moved to another pod,
// is re-established with a growing pause between attempts.
func (s *tunnelSession) forwardWithRetry(ctx context.Context, readyCh chan struct{}) error {
	err := s.kube.PortForward(ctx, s.opts, s.target, s.values.Port, s.stdout, s.status, readyCh)
	if ctx.Err() != nil || err == nil || !isClosed(readyCh) {
		return err
	}
	for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
		delay := reconnectDelay(attempt)
		_, _ = fmt.Fprintf(s.status, "port-forward to %s lost: %v; reconnecting in %s (attempt %d of %d)\n", s.target.ID(), err, delay, attempt, maxReconnectAttempts)
		if !sleepContext(ctx, delay) {
			return ctx.Err()
		}
		err = s.forwardAnnouncingReady(ctx)
		if ctx.Err() != nil || err == nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(s.status, "port-forward to %s lost: %v; giving up after %d reconnect attempts\n", s.target.ID(), err, maxReconnectAttempts)
	return err
}

// forwardAnnouncingReady runs one port-forward attempt and reports as soon as
// the tunnel accepts connections again.
func (s *tunnelSession) forwardAnnouncingReady(ctx context.Context) error {
	ready := make(chan struct{})
	finished := make(chan struct{})
	var announced sync.WaitGroup
	announced.Go(func() {
		select {
		case <-ready:
		case <-finished:
			if !isClosed(ready) {
				return
			}
		}
		_, _ = fmt.Fprintf(s.status, "port-forward to %s re-established\n", s.target.ID())
	})
	err := s.kube.PortForward(ctx, s.opts, s.target, s.values.Port, s.stdout, s.status, ready)
	close(finished)
	announced.Wait()
	return err
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// sleepContext waits for d and reports whether the wait completed before ctx
// ended.
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
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

func secretName(t Target) string {
	if t.SecretName != "" {
		return t.SecretName
	}
	return t.Cluster + "-app"
}

func secretNamespace(t Target) string {
	if t.SecretNamespace != "" {
		return t.SecretNamespace
	}
	return t.Namespace
}
