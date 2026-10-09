package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"

	"github.com/pscheid92/kpg/internal/kpg"
)

func TestResolveServicePodSelectsReadyRunningPod(t *testing.T) {
	c := fakeClient(nil, []runtime.Object{
		rwService("app", "app-db", map[string]string{"cnpg.io/cluster": "app-db"}),
		pod("app", "app-db-1", map[string]string{"cnpg.io/cluster": "app-db"}, corev1.PodRunning, false),
		pod("app", "app-db-2", map[string]string{"cnpg.io/cluster": "app-db"}, corev1.PodRunning, true),
	})

	got, remotePort, err := c.resolveServicePod(context.Background(), kpg.Target{Namespace: "app", Cluster: "app-db"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "app-db-2" {
		t.Fatalf("selected pod %q", got.Name)
	}
	if remotePort != 5432 {
		t.Fatalf("remote port = %d", remotePort)
	}
}

func TestResolveServicePodUsesZalandoRWServiceName(t *testing.T) {
	c := fakeClient(nil, []runtime.Object{
		&corev1.Service{
			Name: "acid-main", Namespace: "legacy",
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"cluster-name": "acid-main", "spilo-role": "master"},
				Ports:    []corev1.ServicePort{{Name: "postgres", Port: 5432}},
			},
		},
		pod("legacy", "acid-main-0", map[string]string{"cluster-name": "acid-main", "spilo-role": "master"}, corev1.PodRunning, true),
	})

	got, _, err := c.resolveServicePod(context.Background(), kpg.Target{
		Provider:    kpg.ProviderZalando,
		Namespace:   "legacy",
		Cluster:     "acid-main",
		ServiceName: "acid-main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "acid-main-0" {
		t.Fatalf("selected pod %q", got.Name)
	}
}

func TestResolveServicePodFallsBackToEndpointSlicesForSelectorlessService(t *testing.T) {
	ready := true
	c := fakeClient(nil, []runtime.Object{
		&corev1.Service{
			Name: "acid-main", Namespace: "legacy",
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "postgres", Port: 5432}}},
		},
		&discoveryv1.EndpointSlice{
			Name:        "acid-main-xyz",
			Namespace:   "legacy",
			Labels:      map[string]string{discoveryv1.LabelServiceName: "acid-main"},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses:  []string{"10.0.0.1"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "acid-main-0", Namespace: "legacy"},
			}},
		},
		pod("legacy", "acid-main-0", map[string]string{"cluster-name": "acid-main", "spilo-role": "master"}, corev1.PodRunning, true),
	})

	got, remotePort, err := c.resolveServicePod(context.Background(), kpg.Target{
		Provider:    kpg.ProviderZalando,
		Namespace:   "legacy",
		Cluster:     "acid-main",
		ServiceName: "acid-main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "acid-main-0" {
		t.Fatalf("selected pod %q", got.Name)
	}
	if remotePort != 5432 {
		t.Fatalf("remote port = %d", remotePort)
	}
}

func TestResolveServicePodErrors(t *testing.T) {
	t.Run("missing service", func(t *testing.T) {
		c := fakeClient(nil, nil)
		_, _, err := c.resolveServicePod(context.Background(), kpg.Target{Namespace: "app", Cluster: "app-db"})
		if !apierrors.IsNotFound(err) {
			t.Fatalf("expected not found, got %v", err)
		}
	})

	t.Run("selectorless service with no endpoints", func(t *testing.T) {
		c := fakeClient(nil, []runtime.Object{
			&corev1.Service{
				Name: "app-db-rw", Namespace: "app",
				Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "postgres", Port: 5432}}},
			},
		})
		_, _, err := c.resolveServicePod(context.Background(), kpg.Target{Namespace: "app", Cluster: "app-db"})
		if err == nil || !strings.Contains(err.Error(), "no selector and no endpoints") {
			t.Fatalf("expected missing-endpoints error, got %v", err)
		}
	})

	t.Run("no running pods", func(t *testing.T) {
		c := fakeClient(nil, []runtime.Object{
			rwService("app", "app-db", map[string]string{"cnpg.io/cluster": "app-db"}),
			pod("app", "app-db-1", map[string]string{"cnpg.io/cluster": "app-db"}, corev1.PodPending, false),
		})
		_, _, err := c.resolveServicePod(context.Background(), kpg.Target{Namespace: "app", Cluster: "app-db"})
		if err == nil || !strings.Contains(err.Error(), "has no running pods") {
			t.Fatalf("expected running pod error, got %v", err)
		}
	})
}

func TestServiceRemotePortSelection(t *testing.T) {
	t.Run("postgres service port with target override", func(t *testing.T) {
		service := rwService("app", "app-db", map[string]string{"app": "postgres"})
		service.Spec.Ports = []corev1.ServicePort{{
			Name:       "postgres",
			Port:       5432,
			TargetPort: intstr.FromInt32(15432),
		}}
		got, err := serviceRemotePort(service)
		if err != nil {
			t.Fatal(err)
		}
		if got != 15432 {
			t.Fatalf("got %d", got)
		}
	})

	t.Run("single service port fallback", func(t *testing.T) {
		service := rwService("app", "app-db", map[string]string{"app": "postgres"})
		service.Spec.Ports = []corev1.ServicePort{{Name: "db", Port: 15432}}
		got, err := serviceRemotePort(service)
		if err != nil {
			t.Fatal(err)
		}
		if got != 15432 {
			t.Fatalf("got %d", got)
		}
	})

	t.Run("ambiguous service ports", func(t *testing.T) {
		service := rwService("app", "app-db", map[string]string{"app": "postgres"})
		service.Spec.Ports = []corev1.ServicePort{{Name: "a", Port: 1111}, {Name: "b", Port: 2222}}
		_, err := serviceRemotePort(service)
		if err == nil || !strings.Contains(err.Error(), "no unambiguous postgres port") {
			t.Fatalf("expected ambiguity, got %v", err)
		}
	})
}

func TestPortForwardURLKeepsServerPathPrefix(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com:6443":                        "https://api.example.com:6443/api/v1/namespaces/app/pods/app-db-1/portforward",
		"https://rancher.example.com/k8s/clusters/c-m-abc123": "https://rancher.example.com/k8s/clusters/c-m-abc123/api/v1/namespaces/app/pods/app-db-1/portforward",
		"https://10.0.0.1:6443/":                              "https://10.0.0.1:6443/api/v1/namespaces/app/pods/app-db-1/portforward",
		"http://localhost:8080":                               "http://localhost:8080/api/v1/namespaces/app/pods/app-db-1/portforward",
	}
	for host, want := range cases {
		got, err := portForwardURL(&rest.Config{Host: host}, "app", "app-db-1")
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if got.String() != want {
			t.Fatalf("%s: url = %q, want %q", host, got.String(), want)
		}
	}
}

func TestPortForwardDialerPrefersWebsocketWithSPDYFallback(t *testing.T) {
	target, err := portForwardURL(&rest.Config{Host: "https://api.example.com"}, "app", "app-db-1")
	if err != nil {
		t.Fatal(err)
	}
	dialer, err := portForwardDialer(&rest.Config{Host: "https://api.example.com"}, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := dialer.(*portforward.StreamingFallbackDialer); !ok {
		t.Fatalf("dialer = %T, want fallback dialer", dialer)
	}
}
