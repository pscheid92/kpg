package kpg

import (
	"context"
	"io"
)

type fakeKube struct {
	targets          []Target
	secrets          map[string]AppSecret
	portForwardCalls int
}

func (f *fakeKube) ListTargets(context.Context, Options) ([]Target, error) {
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
