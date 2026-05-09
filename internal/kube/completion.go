package kube

import (
	"context"
	"sort"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/pscheid92/kpg/internal/kpg"
)

func ContextNames() ([]string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := loadingRules.Load()
	if err != nil {
		return nil, err
	}
	return ContextNamesFromConfig(config), nil
}

func ContextNamesFromConfig(config *clientcmdapi.Config) []string {
	if config == nil {
		return nil
	}
	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func NamespaceNames(ctx context.Context, opts kpg.Options) ([]string, error) {
	client, err := New(opts)
	if err != nil {
		return nil, err
	}
	return client.ListNamespaces(ctx)
}
