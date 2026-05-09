package kube

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pscheid92/kpg/internal/kpg"
)

var cnpgClusterGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "clusters",
}

func (c *Client) targetsFromCNPGList(list unstructured.UnstructuredList) []kpg.Target {
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
		}
		if owner, found, _ := unstructured.NestedString(item.Object, "spec", "bootstrap", "initdb", "owner"); found {
			t.User = owner
		}
		targets = append(targets, t)
	}
	return targets
}
