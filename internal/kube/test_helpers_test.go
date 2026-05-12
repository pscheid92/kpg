package kube

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/pscheid92/kpg/internal/kpg"
)

func fakeClient(clusterObjects []runtime.Object, coreObjects []runtime.Object) *Client {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			cnpgClusterGVR:       "ClusterList",
			zalandoPostgresqlGVR: "PostgresqlList",
		},
		clusterObjects...,
	)
	core := k8sfake.NewSimpleClientset(coreObjects...)
	registerInstalledResources(core, cnpgClusterGVR, zalandoPostgresqlGVR)
	return &Client{
		dynamic: dynamicClient,
		core:    core,
	}
}

func registerInstalledResources(core *k8sfake.Clientset, gvrs ...schema.GroupVersionResource) {
	discovery := core.Discovery().(*fakediscovery.FakeDiscovery)
	byGV := map[string]*metav1.APIResourceList{}
	for _, gvr := range gvrs {
		key := gvr.GroupVersion().String()
		list, ok := byGV[key]
		if !ok {
			list = &metav1.APIResourceList{GroupVersion: key}
			byGV[key] = list
		}
		list.APIResources = append(list.APIResources, metav1.APIResource{Name: gvr.Resource, Namespaced: true})
	}
	for _, list := range byGV {
		discovery.Resources = append(discovery.Resources, list)
	}
}

func cnpgCluster(namespace string, name string, database string, owner string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Cluster",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"bootstrap": map[string]any{
				"initdb": map[string]any{
					"database": database,
					"owner":    owner,
				},
			},
		},
	}}
}

func zalandoCluster(namespace string, name string, databases map[string]string, users map[string][]string) *unstructured.Unstructured {
	databaseObject := make(map[string]any, len(databases))
	for database, user := range databases {
		databaseObject[database] = user
	}
	userObject := make(map[string]any, len(users))
	for user, flags := range users {
		values := make([]any, 0, len(flags))
		for _, flag := range flags {
			values = append(values, flag)
		}
		userObject[user] = values
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"databases": databaseObject,
			"users":     userObject,
		},
	}}
}

func rwService(namespace string, cluster string, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: cluster + "-rw", Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{{
				Name: "postgres",
				Port: 5432,
			}},
		},
	}
}

func pod(namespace string, name string, labels map[string]string, phase corev1.PodPhase, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Status: corev1.PodStatus{
			Phase: phase,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: status,
			}},
		},
	}
}

func targetIDs(targets []kpg.Target) []string {
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID())
	}
	return ids
}
