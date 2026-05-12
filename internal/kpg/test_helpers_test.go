package kpg

import (
	"context"
	"io"
)

type fakeKube struct {
	targets          []Target
	secrets          map[string]AppSecret
	portForwardCalls int
	listOptions      []Options
}

func (f *fakeKube) ListTargets(_ context.Context, opts Options) ([]Target, error) {
	f.listOptions = append(f.listOptions, opts)
	return append([]Target(nil), f.targets...), nil
}

func (f *fakeKube) ReadCredentials(_ context.Context, _ Options, t Target) (AppSecret, bool, error) {
	secret, ok := f.secrets[t.ID()]
	return secret, ok, nil
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
