package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	streamhttp "k8s.io/streaming/pkg/httpstream"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (c *Client) PortForward(ctx context.Context, _ kpg.Options, t kpg.Target, localPort int, _ io.Writer, errOut io.Writer, readyCh chan struct{}) error {
	pod, remotePort, err := c.resolveServicePod(ctx, t)
	if err != nil {
		return err
	}
	target, err := portForwardURL(c.restConfig, t.Namespace, pod.Name)
	if err != nil {
		return err
	}
	dialer, err := portForwardDialer(c.restConfig, target)
	if err != nil {
		return err
	}
	if readyCh == nil {
		readyCh = make(chan struct{})
	}
	ports := []string{strconv.Itoa(localPort) + ":" + strconv.Itoa(int(remotePort))}
	forwarder, err := portforward.NewOnAddressesForStreamingWithContext(ctx, dialer, []string{"127.0.0.1"}, ports, readyCh, io.Discard, errOut)
	if err != nil {
		return err
	}
	err = forwarder.ForwardPorts()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// portForwardURL builds the pod portforward subresource URL the way the REST
// client does, so API servers behind a path prefix (Rancher, some ingress
// setups) and hosts written with a trailing slash keep working.
func portForwardURL(config *rest.Config, namespace string, pod string) (*url.URL, error) {
	cfg := rest.CopyConfig(config)
	cfg.APIPath = "/api"
	cfg.GroupVersion = &corev1.SchemeGroupVersion
	base, versionedAPIPath, err := rest.DefaultServerUrlFor(cfg)
	if err != nil {
		return nil, err
	}
	target := *base
	target.Path = path.Join(base.Path, versionedAPIPath, "namespaces", namespace, "pods", pod, "portforward")
	return &target, nil
}

// portForwardDialer prefers the WebSocket tunnel that current kubectl uses
// and falls back to SPDY for API servers or proxies that reject the upgrade.
func portForwardDialer(config *rest.Config, target *url.URL) (streamhttp.Dialer, error) {
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, err
	}
	spdyDialer := spdy.NewDialerForStreaming(upgrader, &http.Client{Transport: transport}, http.MethodPost, target)
	websocketDialer, err := portforward.NewSPDYOverWebsocketDialerForStreaming(target, config)
	if err != nil {
		return nil, err
	}
	return portforward.NewFallbackDialerForStreaming(websocketDialer, spdyDialer, func(err error) bool {
		return streamhttp.IsUpgradeFailure(err) || streamhttp.IsHTTPSProxyError(err)
	}), nil
}

func (c *Client) resolveServicePod(ctx context.Context, t kpg.Target) (*corev1.Pod, int32, error) {
	serviceName := t.ServiceName
	if serviceName == "" {
		serviceName = t.Cluster + "-rw"
	}
	service, err := c.core.CoreV1().Services(t.Namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, 0, err
	}
	remotePort, err := serviceRemotePort(service)
	if err != nil {
		return nil, 0, err
	}
	pods, err := c.podsForService(ctx, t.Namespace, service)
	if err != nil {
		return nil, 0, err
	}
	pod, err := pickPodForPortForward(pods, t.Namespace, serviceName)
	if err != nil {
		return nil, 0, err
	}
	return pod, remotePort, nil
}

func (c *Client) podsForService(ctx context.Context, namespace string, service *corev1.Service) ([]corev1.Pod, error) {
	if len(service.Spec.Selector) > 0 {
		selector := labels.SelectorFromSet(service.Spec.Selector).String()
		list, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	}
	return c.podsFromEndpointSlices(ctx, namespace, service.Name)
}

func (c *Client) podsFromEndpointSlices(ctx context.Context, namespace, serviceName string) ([]corev1.Pod, error) {
	slices, err := c.core.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + serviceName,
	})
	if err != nil {
		return nil, err
	}
	if len(slices.Items) == 0 {
		return nil, fmt.Errorf("service %s/%s has no selector and no endpoints", namespace, serviceName)
	}
	names := endpointSlicePodNames(slices.Items)
	if len(names) == 0 {
		return nil, fmt.Errorf("service %s/%s has no endpoints with pod targets", namespace, serviceName)
	}
	pods := make([]corev1.Pod, 0, len(names))
	for _, name := range names {
		pod, err := c.core.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		pods = append(pods, *pod)
	}
	return pods, nil
}

func endpointSlicePodNames(endpointSlices []discoveryv1.EndpointSlice) []string {
	var names []string
	seen := map[string]struct{}{}
	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef == nil || endpoint.TargetRef.Kind != "Pod" || endpoint.TargetRef.Name == "" {
				continue
			}
			if _, ok := seen[endpoint.TargetRef.Name]; ok {
				continue
			}
			seen[endpoint.TargetRef.Name] = struct{}{}
			names = append(names, endpoint.TargetRef.Name)
		}
	}
	return names
}

func pickPodForPortForward(pods []corev1.Pod, namespace, serviceName string) (*corev1.Pod, error) {
	candidates := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil {
			candidates = append(candidates, pod)
		}
	}
	slices.SortFunc(candidates, func(a, b corev1.Pod) int {
		return strings.Compare(a.Name, b.Name)
	})
	for i := range candidates {
		if podReady(&candidates[i]) {
			return &candidates[i], nil
		}
	}
	if len(candidates) > 0 {
		return &candidates[0], nil
	}
	return nil, fmt.Errorf("service %s/%s has no running pods", namespace, serviceName)
}

func serviceRemotePort(service *corev1.Service) (int32, error) {
	for _, port := range service.Spec.Ports {
		if port.Port == 5432 {
			return targetPortValue(port.TargetPort, port.Port), nil
		}
	}
	if len(service.Spec.Ports) == 1 {
		port := service.Spec.Ports[0]
		return targetPortValue(port.TargetPort, port.Port), nil
	}
	return 0, fmt.Errorf("service %s/%s has no unambiguous postgres port", service.Namespace, service.Name)
}

func targetPortValue(target intstr.IntOrString, fallback int32) int32 {
	if target.Type == intstr.Int && target.IntVal > 0 {
		return target.IntVal
	}
	return fallback
}

func podReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
