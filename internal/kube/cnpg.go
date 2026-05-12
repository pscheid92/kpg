package kube

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pscheid92/kpg/internal/kpg"
)

type cnpgProvider struct{}

var cnpgClusterGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "clusters",
}

func (cnpgProvider) name() string {
	return kpg.ProviderCNPG
}

func (cnpgProvider) gvr() schema.GroupVersionResource {
	return cnpgClusterGVR
}

func (cnpgProvider) targets(list unstructured.UnstructuredList) []kpg.Target {
	targets := make([]kpg.Target, 0, len(list.Items))
	for _, item := range list.Items {
		name := item.GetName()
		ns := item.GetNamespace()
		if name == "" || ns == "" {
			continue
		}
		t := kpg.Target{
			Provider:        kpg.ProviderCNPG,
			Namespace:       ns,
			Cluster:         name,
			ServiceName:     name + "-rw",
			SecretName:      name + "-app",
			SecretNamespace: ns,
		}
		if database, found, _ := unstructured.NestedString(item.Object, "spec", "bootstrap", "initdb", "database"); found {
			t.Database = database
			t.DatabaseOptions = []string{database}
		}
		if owner, found, _ := unstructured.NestedString(item.Object, "spec", "bootstrap", "initdb", "owner"); found {
			t.User = owner
			t.UserOptions = []string{owner}
		}
		targets = append(targets, t)
	}
	return targets
}

func (cnpgProvider) enrichTarget(_ context.Context, _ *Client, t kpg.Target) (kpg.Target, error) {
	return t, nil
}

func (p cnpgProvider) resolveConnection(ctx context.Context, c *Client, opts kpg.Options, t kpg.Target) (kpg.Target, kpg.AppSecret, error) {
	return resolveConnectionWith(ctx, c, opts, t, p.applyConnectionOptions)
}

func (cnpgProvider) applyConnectionOptions(t kpg.Target, opts kpg.Options) kpg.Target {
	if opts.Database != "" {
		t.Database = opts.Database
	}
	if opts.User != "" {
		t.User = opts.User
		t.SecretName = t.Cluster + "-" + opts.User
		t.SecretNamespace = t.Namespace
	}
	return t
}
