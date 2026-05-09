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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (c *Client) PortForward(ctx context.Context, opts kpg.Options, t kpg.Target, localPort int, out io.Writer, errOut io.Writer, readyCh chan struct{}) error {
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
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, ports, stopCh, readyCh, errOut, errOut)
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
	if len(service.Spec.Selector) == 0 {
		return nil, 0, fmt.Errorf("service %s/%s has no selector", t.Namespace, serviceName)
	}
	remotePort, err := serviceRemotePort(service)
	if err != nil {
		return nil, 0, err
	}
	selector := labels.SelectorFromSet(service.Spec.Selector).String()
	pods, err := c.core.CoreV1().Pods(t.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, 0, err
	}
	candidates := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil {
			candidates = append(candidates, pod)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Name < candidates[j].Name
	})
	for i := range candidates {
		if podReady(&candidates[i]) {
			return &candidates[i], remotePort, nil
		}
	}
	if len(candidates) > 0 {
		return &candidates[0], remotePort, nil
	}
	return nil, 0, fmt.Errorf("service %s/%s has no running pods", t.Namespace, serviceName)
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
