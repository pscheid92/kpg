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

func (f *fakeKube) ListClusterUsers(_ context.Context, t Target) ([]string, error) {
	if f.clusterUsers == nil {
		return nil, nil
	}
	return append([]string(nil), f.clusterUsers[t.ID()]...), nil
}

func (f *fakeKube) ReadCredentials(_ context.Context, _ Options, t Target) (AppSecret, bool, error) {
	if f.secretsByName != nil {
		if secret, ok := f.secretsByName[t.SecretName]; ok {
			return secret, true, nil
		}
	}
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
