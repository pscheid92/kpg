package kpg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// failoverCluster is a fake Kube whose read-write service can move between
// pods and whose port-forwards can be broken, like a CloudNativePG failover.
type failoverCluster struct {
	mu       sync.Mutex
	pod      string
	ready    bool
	err      error
	kills    map[string]chan struct{}
	forwards map[string]int
}

func newFailoverCluster(pod string) *failoverCluster {
	return &failoverCluster{pod: pod, ready: true, kills: map[string]chan struct{}{}, forwards: map[string]int{}}
}

func (c *failoverCluster) setPrimary(pod string, ready bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pod, c.ready, c.err = pod, ready, err
}

// breakForward ends the port-forwards to pod, like a deleted pod would.
func (c *failoverCluster) breakForward(pod string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kill, ok := c.kills[pod]; ok {
		close(kill)
		delete(c.kills, pod)
	}
}

func (c *failoverCluster) forwardCount(pod string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.forwards[pod]
}

func (c *failoverCluster) ListTargets(context.Context, Options) ([]Target, error) { return nil, nil }
func (c *failoverCluster) EnrichTarget(_ context.Context, t Target) (Target, error) {
	return t, nil
}
func (c *failoverCluster) ResolveConnection(_ context.Context, _ Options, t Target) (Target, AppSecret, error) {
	return t, AppSecret{}, nil
}

func (c *failoverCluster) ServicePod(context.Context, Target) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return "", false, c.err
	}
	return c.pod, c.ready, nil
}

func (c *failoverCluster) PortForward(ctx context.Context, _ Options, _ Target, pod string, localPort int, _ io.Writer, readyCh chan struct{}) error {
	c.mu.Lock()
	kill, ok := c.kills[pod]
	if !ok {
		kill = make(chan struct{})
		c.kills[pod] = kill
	}
	c.forwards[pod]++
	c.mu.Unlock()
	return servePod(ctx, pod, localPort, readyCh, kill)
}

func fastProxyTimings(t *testing.T, hold time.Duration) {
	t.Helper()
	oldHold, oldRecheck, oldPoll := holdTimeout, recheckAfter, pollInterval
	holdTimeout, recheckAfter, pollInterval = hold, 0, 10*time.Millisecond
	t.Cleanup(func() { holdTimeout, recheckAfter, pollInterval = oldHold, oldRecheck, oldPoll })
}

type proxyHarness struct {
	cluster *failoverCluster
	proxy   *tunnelProxy
	addr    string
	status  *bytes.Buffer
}

func startProxy(t *testing.T, cluster *failoverCluster) *proxyHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	status := &bytes.Buffer{}
	values := EnvValues{Host: "127.0.0.1", Port: port, User: "app", Database: "app"}
	proxy := newTunnelProxy(ctx, cluster, Options{}, Target{Namespace: "app", Cluster: "app-db"}, values, &lockedWriter{w: status})
	if err := proxy.establish(ctx); err != nil {
		t.Fatal(err)
	}
	go proxy.serve(ln)
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		proxy.close()
	})
	return &proxyHarness{cluster: cluster, proxy: proxy, addr: ln.Addr().String(), status: status}
}

func (h *proxyHarness) statusText() string {
	h.proxy.status.(*lockedWriter).mu.Lock()
	defer h.proxy.status.(*lockedWriter).mu.Unlock()
	return h.status.String()
}

// dial connects through the proxy and returns the connection and the pod
// that answered.
func (h *proxyHarness) dial(t *testing.T) (net.Conn, string, error) {
	t.Helper()
	conn, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, "", err
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, strings.TrimSpace(line), nil
}

func echoes(conn net.Conn, text string) bool {
	if _, err := conn.Write([]byte(text)); err != nil {
		return false
	}
	buf := make([]byte, len(text))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := io.ReadFull(conn, buf)
	return err == nil && string(buf) == text
}

func TestProxyConnectsToTheCurrentPrimary(t *testing.T) {
	fastProxyTimings(t, time.Second)
	h := startProxy(t, newFailoverCluster("db-1"))
	conn, pod, err := h.dial(t)
	if err != nil || pod != "db-1" {
		t.Fatalf("pod = %q, err = %v", pod, err)
	}
	if !echoes(conn, "select 1;") {
		t.Fatal("data did not flow through the proxy")
	}
}

func TestProxySendsNewConnectionsToTheNewPrimaryAndKeepsOldOnes(t *testing.T) {
	fastProxyTimings(t, time.Second)
	h := startProxy(t, newFailoverCluster("db-1"))
	oldConn, pod, err := h.dial(t)
	if err != nil || pod != "db-1" {
		t.Fatalf("first pod = %q, err = %v", pod, err)
	}

	h.cluster.setPrimary("db-2", true, nil)
	_, pod, err = h.dial(t)
	if err != nil || pod != "db-2" {
		t.Fatalf("new connection went to %q, err = %v; want db-2", pod, err)
	}
	if !echoes(oldConn, "still here") {
		t.Fatal("the existing connection to db-1 was dropped by the switch")
	}
	if !strings.Contains(h.statusText(), "re-established via pod db-2") {
		t.Fatalf("missing switch message:\n%s", h.statusText())
	}
}

func TestProxyHoldsConnectionsDuringFailover(t *testing.T) {
	fastProxyTimings(t, 5*time.Second)
	h := startProxy(t, newFailoverCluster("db-1"))
	if _, pod, err := h.dial(t); err != nil || pod != "db-1" {
		t.Fatalf("first pod = %q, err = %v", pod, err)
	}

	// The primary goes away: its port-forward breaks and no pod is ready.
	h.cluster.setPrimary("", false, errors.New("service app/app-db-rw has no running pods"))
	h.cluster.breakForward("db-1")

	type result struct {
		pod string
		err error
	}
	got := make(chan result, 1)
	started := time.Now()
	go func() {
		_, pod, err := h.dial(t)
		got <- result{pod, err}
	}()

	time.Sleep(300 * time.Millisecond)
	select {
	case r := <-got:
		t.Fatalf("connection completed during the failover: %+v", r)
	default:
	}
	h.cluster.setPrimary("db-2", true, nil)

	r := <-got
	if r.err != nil || r.pod != "db-2" {
		t.Fatalf("held connection reached %q, err = %v; want db-2", r.pod, r.err)
	}
	if waited := time.Since(started); waited < 300*time.Millisecond {
		t.Fatalf("connection was not held, waited %s", waited)
	}
	status := h.statusText()
	for _, want := range []string{
		"lost (pod db-1)",
		"no ready primary for app/app-db yet",
		"holding new connections",
		"re-established via pod db-2",
	} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}
}

func TestProxyDoesNotUseAPrimaryThatIsShuttingDown(t *testing.T) {
	fastProxyTimings(t, 5*time.Second)
	h := startProxy(t, newFailoverCluster("db-1"))
	if _, _, err := h.dial(t); err != nil {
		t.Fatal(err)
	}
	// The old primary is still forwarded but no longer ready, like a pod in
	// its shutdown phase. New connections must wait instead of reaching it.
	h.cluster.setPrimary("db-1", false, nil)
	got := make(chan string, 1)
	go func() {
		_, pod, _ := h.dial(t)
		got <- pod
	}()
	time.Sleep(200 * time.Millisecond)
	select {
	case pod := <-got:
		t.Fatalf("connection reached %q while the primary was not ready", pod)
	default:
	}
	h.cluster.setPrimary("db-3", true, nil)
	if pod := <-got; pod != "db-3" {
		t.Fatalf("held connection reached %q, want db-3", pod)
	}
}

func TestProxyGivesUpAfterTheHoldTimeoutAndPrintsTheHint(t *testing.T) {
	fastProxyTimings(t, 200*time.Millisecond)
	h := startProxy(t, newFailoverCluster("db-1"))
	h.cluster.setPrimary("", false, errors.New("no running pods"))
	h.cluster.breakForward("db-1")

	started := time.Now()
	if _, pod, err := h.dial(t); err == nil {
		t.Fatalf("connection should be closed after the hold timeout, reached %q", pod)
	}
	if waited := time.Since(started); waited < 200*time.Millisecond {
		t.Fatalf("closed after %s, before the hold timeout", waited)
	}
	status := h.statusText()
	for _, want := range []string{"no ready primary for app/app-db within", "closed a waiting connection", `in psql run: \c "host=127.0.0.1 port=`} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}
}

func TestProxyStopsAnUnusedOldForward(t *testing.T) {
	fastProxyTimings(t, time.Second)
	h := startProxy(t, newFailoverCluster("db-1"))
	conn, _, err := h.dial(t)
	if err != nil {
		t.Fatal(err)
	}
	h.cluster.setPrimary("db-2", true, nil)
	if _, pod, err := h.dial(t); err != nil || pod != "db-2" {
		t.Fatalf("pod = %q, err = %v", pod, err)
	}
	if n := h.liveBackends(); n != 2 {
		t.Fatalf("live forwards = %d, want 2 while db-1 still has a connection", n)
	}
	_ = conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for h.liveBackends() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("the forward to db-1 was not stopped after its last connection closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (h *proxyHarness) liveBackends() int {
	h.proxy.mu.Lock()
	defer h.proxy.mu.Unlock()
	return len(h.proxy.backends)
}

func TestProxyReusesTheForwardWhileThePrimaryIsUnchanged(t *testing.T) {
	fastProxyTimings(t, time.Second)
	h := startProxy(t, newFailoverCluster("db-1"))
	for range 5 {
		if _, pod, err := h.dial(t); err != nil || pod != "db-1" {
			t.Fatalf("pod = %q, err = %v", pod, err)
		}
	}
	if n := h.cluster.forwardCount("db-1"); n != 1 {
		t.Fatalf("opened %d port-forwards to db-1, want 1", n)
	}
}

func TestProxyEstablishUsesARunningPodThatIsNotReadyYet(t *testing.T) {
	fastProxyTimings(t, time.Second)
	cluster := newFailoverCluster("db-1")
	cluster.setPrimary("db-1", false, nil)
	h := startProxy(t, cluster)
	if n := cluster.forwardCount("db-1"); n != 1 {
		t.Fatalf("initial forward count = %d", n)
	}
	_ = h
}
