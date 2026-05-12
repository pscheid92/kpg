package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	var targets []kpg.Target
	var forbidden []forbiddenAttempt
	for _, provider := range registeredProviders() {
		installed, err := c.resourceInstalled(provider.gvr())
		if err != nil {
			return nil, err
		}
		if !installed {
			continue
		}
		attempted := namespace
		list, err := c.listProvider(ctx, provider, namespace)
		if shouldRetryInDefaultNamespace(err, opts.Namespace, namespace, c.defaultNamespace) {
			attempted = c.defaultNamespace
			list, err = c.listProvider(ctx, provider, c.defaultNamespace)
		}
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			if apierrors.IsForbidden(err) {
				forbidden = append(forbidden, forbiddenAttempt{gvr: provider.gvr(), namespace: attempted})
				continue
			}
			return nil, err
		}
		if list != nil {
			targets = append(targets, provider.targets(*list)...)
		}
	}
	if len(targets) == 0 && len(forbidden) > 0 {
		return nil, forbiddenListError(forbidden)
	}
	return targets, nil
}

func (c *Client) listProvider(ctx context.Context, provider postgresProvider, namespace string) (*unstructured.UnstructuredList, error) {
	return c.dynamic.Resource(provider.gvr()).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

func (c *Client) resourceInstalled(gvr schema.GroupVersionResource) (bool, error) {
	list, err := c.core.Discovery().ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	for _, resource := range list.APIResources {
		if resource.Name == gvr.Resource {
			return true, nil
		}
	}
	return false, nil
}

func shouldRetryInDefaultNamespace(err error, requestedNamespace string, listedNamespace string, defaultNamespace string) bool {
	return err != nil &&
		requestedNamespace == "" &&
		listedNamespace == metav1.NamespaceAll &&
		defaultNamespace != "" &&
		apierrors.IsForbidden(err)
}

type forbiddenAttempt struct {
	gvr       schema.GroupVersionResource
	namespace string
}

func forbiddenListError(attempts []forbiddenAttempt) error {
	resources := make([]string, 0, len(attempts))
	for _, a := range attempts {
		resources = append(resources, a.gvr.Resource+"."+a.gvr.Group)
	}
	namespace := attempts[0].namespace
	scope := fmt.Sprintf("namespace %q", namespace)
	if namespace == metav1.NamespaceAll {
		scope = "the cluster scope"
	}
	return fmt.Errorf("no permission to list %s in %s; pass -n <namespace> to try a different namespace", strings.Join(resources, " or "), scope)
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
