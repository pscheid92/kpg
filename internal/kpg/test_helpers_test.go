package kpg

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
)

type fakeKube struct {
	targets       []Target
	secrets       map[string]AppSecret
	secretsByName map[string]AppSecret
	clusterUsers  map[string][]string
	listOptions   []Options
	// portForwardScript gives each PortForward call its behaviour; calls past
	// the end of the script serve a real local listener until cancelled.
	portForwardScript []func(ready chan struct{}) error

	mu               sync.Mutex
	portForwardCalls int
}

func (f *fakeKube) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.portForwardCalls
}

func (f *fakeKube) ServicePod(_ context.Context, t Target) (string, bool, error) {
	return t.Cluster + "-1", true, nil
}

func (f *fakeKube) ListTargets(_ context.Context, opts Options) ([]Target, error) {
	f.listOptions = append(f.listOptions, opts)
	return append([]Target(nil), f.targets...), nil
}

func (f *fakeKube) EnrichTarget(_ context.Context, t Target) (Target, error) {
	if f.clusterUsers == nil {
		return t, nil
	}
	t.UserOptions = mergeStringOptions(t.UserOptions, f.clusterUsers[t.ID()])
	return t, nil
}

func (f *fakeKube) ResolveConnection(_ context.Context, opts Options, t Target) (Target, AppSecret, error) {
	t = ApplyConnectionOverrides(t, opts)
	if f.secretsByName != nil {
		if secret, ok := f.secretsByName[t.SecretName]; ok {
			t = ApplySecret(t, secret)
			t = ApplyConnectionOverrides(t, opts)
			return t, secret, nil
		}
	}
	secret, ok := f.secrets[t.ID()]
	if ok {
		t = ApplySecret(t, secret)
		t = ApplyConnectionOverrides(t, opts)
	}
	return t, secret, nil
}

func mergeStringOptions(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	merged := make([]string, 0, len(a)+len(b))
	for _, values := range [][]string{a, b} {
		for _, value := range values {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
}

func (f *fakeKube) PortForward(ctx context.Context, _ Options, _ Target, pod string, localPort int, _ io.Writer, readyCh chan struct{}) error {
	f.mu.Lock()
	call := f.portForwardCalls
	f.portForwardCalls++
	f.mu.Unlock()
	if call < len(f.portForwardScript) {
		return f.portForwardScript[call](readyCh)
	}
	return servePod(ctx, pod, localPort, readyCh, nil)
}

// servePod stands in for a port-forward: it listens on localPort, answers
// every connection with the pod name, and echoes what the client sends. It
// returns when ctx ends or kill is closed, closing its open connections the
// way a broken port-forward would.
func servePod(ctx context.Context, pod string, localPort int, readyCh chan struct{}, kill <-chan struct{}) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)))
	if err != nil {
		return err
	}
	var mu sync.Mutex
	var conns []net.Conn
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			wg.Go(func() {
				_, _ = conn.Write([]byte(pod + "\n"))
				_, _ = io.Copy(conn, conn)
			})
		}
	})
	close(readyCh)
	var result error
	select {
	case <-ctx.Done():
		result = ctx.Err()
	case <-kill:
		result = errLostPod
	}
	_ = ln.Close()
	mu.Lock()
	for _, conn := range conns {
		_ = conn.Close()
	}
	mu.Unlock()
	wg.Wait()
	return result
}

type fakeError string

func (e fakeError) Error() string { return string(e) }

const errLostPod = fakeError("lost connection to pod")

// renderedOutput collects --output mode output and ends the session once the
// connection values are written, standing in for the user's Ctrl-C: with the
// proxy, render mode serves the tunnel until it is interrupted.
type renderedOutput struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (r *renderedOutput) Write(p []byte) (int, error) {
	n, err := r.Buffer.Write(p)
	r.cancel()
	return n, err
}

func untilRendered(t *testing.T) (context.Context, *renderedOutput) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx, &renderedOutput{cancel: cancel}
}
