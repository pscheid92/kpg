package kube

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pscheid92/kpg/internal/kpg"
)

type postgresProvider interface {
	name() string
	gvr() schema.GroupVersionResource
	targets(unstructured.UnstructuredList) []kpg.Target
	enrichTarget(context.Context, *Client, kpg.Target) (kpg.Target, error)
	resolveConnection(context.Context, *Client, kpg.Options, kpg.Target) (kpg.Target, kpg.AppSecret, error)
}

func registeredProviders() []postgresProvider {
	return []postgresProvider{
		cnpgProvider{},
		zalandoProvider{},
	}
}

func (c *Client) providerFor(name string) postgresProvider {
	for _, provider := range registeredProviders() {
		if provider.name() == name {
			return provider
		}
	}
	if name == "" {
		return cnpgProvider{}
	}
	return nil
}
