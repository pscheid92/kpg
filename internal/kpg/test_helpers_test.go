package kpg

import (
	"context"
	"io"
)

type fakeKube struct {
	targets          []Target
	secrets          map[string]AppSecret
	secretsByName    map[string]AppSecret
	clusterUsers     map[string][]string
	portForwardCalls int
	listOptions      []Options
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

func (f *fakeKube) PortForward(ctx context.Context, _ Options, _ Target, _ int, _ io.Writer, _ io.Writer, readyCh chan struct{}) error {
	f.portForwardCalls++
	if readyCh != nil {
		close(readyCh)
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
