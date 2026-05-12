package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (c *Client) PortForward(ctx context.Context, opts kpg.Options, t kpg.Target, localPort int, _ io.Writer, errOut io.Writer, readyCh chan struct{}) error {
	pod, remotePort, err := c.resolveServicePod(ctx, t)
	if err != nil {
		return err
	}

	restConfig := rest.CopyConfig(c.restConfig)
	restConfig.APIPath = "/api"
	restConfig.GroupVersion = &corev1.SchemeGroupVersion
	restConfig.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	roundTripper, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", t.Namespace, pod.Name)
	hostIP := strings.TrimPrefix(restConfig.Host, "https://")
	hostIP = strings.TrimPrefix(hostIP, "http://")
	serverURL := &url.URL{Scheme: "https", Path: path, Host: hostIP}
	if strings.HasPrefix(restConfig.Host, "http://") {
		serverURL.Scheme = "http"
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)

	stopCh := make(chan struct{})
	if readyCh == nil {
		readyCh = make(chan struct{})
	}
	go func() {
		<-ctx.Done()
		close(stopCh)
	}()

	ports := []string{strconv.Itoa(localPort) + ":" + strconv.Itoa(int(remotePort))}
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, ports, stopCh, readyCh, io.Discard, errOut)
	if err != nil {
		return err
	}
	err = forwarder.ForwardPorts()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
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

func endpointSlicePodNames(slices []discoveryv1.EndpointSlice) []string {
	var names []string
	seen := map[string]struct{}{}
	for _, slice := range slices {
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
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Name < candidates[j].Name
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
