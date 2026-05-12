package kube

import (
	"context"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/pscheid92/kpg/internal/kpg"
)

type Client struct {
	restConfig       *rest.Config
	dynamic          dynamic.Interface
	core             kubernetes.Interface
	defaultNamespace string
}

type provider struct {
	gvr     schema.GroupVersionResource
	targets func(unstructured.UnstructuredList) []kpg.Target
}

func New(opts kpg.Options) (*Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: opts.Context}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &Client{restConfig: config, dynamic: dyn, core: core, defaultNamespace: namespace}, nil
}

func (c *Client) ListTargets(ctx context.Context, opts kpg.Options) ([]kpg.Target, error) {
	namespace := opts.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}
	providers := []provider{
		{gvr: cnpgClusterGVR, targets: c.targetsFromCNPGList},
		{gvr: zalandoPostgresqlGVR, targets: c.targetsFromZalandoList},
	}
	var targets []kpg.Target
	for _, provider := range providers {
		list, err := c.listProvider(ctx, provider, namespace)
		if shouldRetryInDefaultNamespace(err, opts.Namespace, namespace, c.defaultNamespace) {
			list, err = c.listProvider(ctx, provider, c.defaultNamespace)
		}
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if list != nil {
			targets = append(targets, provider.targets(*list)...)
		}
	}
	return targets, nil
}

func (c *Client) listProvider(ctx context.Context, provider provider, namespace string) (*unstructured.UnstructuredList, error) {
	return c.dynamic.Resource(provider.gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

func shouldRetryInDefaultNamespace(err error, requestedNamespace string, listedNamespace string, defaultNamespace string) bool {
	return err != nil &&
		requestedNamespace == "" &&
		listedNamespace == metav1.NamespaceAll &&
		defaultNamespace != "" &&
		apierrors.IsForbidden(err)
}

func (c *Client) ListNamespaces(ctx context.Context) ([]string, error) {
	list, err := c.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, namespace := range list.Items {
		if namespace.Name != "" {
			names = append(names, namespace.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}
