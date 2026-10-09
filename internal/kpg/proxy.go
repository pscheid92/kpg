package kpg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Tunables for the proxy. Tests shorten them.
var (
	// holdTimeout is how long a new connection waits for a ready primary
	// while the cluster fails over.
	holdTimeout = 60 * time.Second
	// recheckAfter is how long a readiness verdict for the current pod is
	// trusted before the next new connection checks the cluster again.
	recheckAfter = 2 * time.Second
	// pollInterval is the pause between readiness checks while connections
	// are held.
	pollInterval = time.Second
)

// tunnelProxy owns the local port the client connects to. Every accepted
// connection is spliced onto a port-forward to the pod that is currently the
// ready primary behind the read-write service:
//
//   - Existing connections stay on the pod they started on until the server
//     or the client ends them.
//   - New connections go to the current ready primary. When the primary moved,
//     a new port-forward is opened for them.
//   - While no primary is ready, for example during a failover, new
//     connections are held open and connected as soon as one is. Clients
//     therefore see a slow connect instead of a refused one, and their own
//     reconnect logic succeeds.
type tunnelProxy struct {
	ctx    context.Context
	stop   context.CancelFunc
	kube   Kube
	opts   Options
	target Target
	values EnvValues
	status io.Writer

	flight sync.Mutex // serializes backend selection

	mu        sync.Mutex
	current   *backend
	checkedAt time.Time
	waiting   bool // a hold episode was announced
	lost      bool // a backend was lost since the last re-establish message
	backends  map[*backend]struct{}
}

// backend is one port-forward to one pod on an internal local port.
type backend struct {
	pod    string
	port   int
	cancel context.CancelFunc
	done   chan struct{}
	ready  bool
	active int
	dead   bool
	err    error // why the port-forward ended, once dead
}

func newTunnelProxy(ctx context.Context, kube Kube, opts Options, target Target, values EnvValues, status io.Writer) *tunnelProxy {
	ctx, stop := context.WithCancel(ctx)
	return &tunnelProxy{
		ctx:      ctx,
		stop:     stop,
		kube:     kube,
		opts:     opts,
		target:   target,
		values:   values,
		status:   status,
		backends: map[*backend]struct{}{},
	}
}

// establish opens the first port-forward. Unlike later connections it does
// not wait for readiness: a running pod that is not ready yet is still used,
// as kpg always did.
func (p *tunnelProxy) establish(ctx context.Context) error {
	p.flight.Lock()
	defer p.flight.Unlock()
	pod, _, err := p.kube.ServicePod(ctx, p.target)
	if err != nil {
		return err
	}
	b, err := p.start(ctx, pod)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.current = b
	p.checkedAt = time.Now()
	p.mu.Unlock()
	p.reportIfDied(b)
	return nil
}

// serve accepts connections until the listener is closed.
func (p *tunnelProxy) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go p.handle(conn)
	}
}

// close ends the session's proxy: it stops every port-forward and waits for
// them to end. The proxy's context is cancelled first so that stopping is not
// reported as a lost tunnel.
func (p *tunnelProxy) close() {
	p.stop()
	p.mu.Lock()
	all := make([]*backend, 0, len(p.backends))
	for b := range p.backends {
		all = append(all, b)
	}
	p.mu.Unlock()
	for _, b := range all {
		b.cancel()
		<-b.done
	}
}

func (p *tunnelProxy) handle(client net.Conn) {
	defer func() { _ = client.Close() }()
	deadline := time.Now().Add(holdTimeout)
	for {
		b, err := p.backendFor(deadline)
		if err != nil {
			if p.ctx.Err() == nil {
				_, _ = fmt.Fprintf(p.status, "no ready primary for %s within %s: %v; closed a waiting connection\n", p.target.ID(), holdTimeout, err)
				_, _ = fmt.Fprintf(p.status, "reconnect once the cluster is healthy; in psql run: %s\n", psqlReconnectCommand(p.values))
			}
			return
		}
		upstream, err := (&net.Dialer{}).DialContext(p.ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.port)))
		if err != nil {
			p.release(b)
			p.invalidate(b)
			if time.Now().After(deadline) || p.ctx.Err() != nil {
				return
			}
			continue
		}
		splice(client, upstream)
		p.release(b)
		return
	}
}

// backendFor returns the port-forward a new connection should use and
// counts the connection on it. It waits for a ready primary until deadline.
func (p *tunnelProxy) backendFor(deadline time.Time) (*backend, error) {
	p.flight.Lock()
	defer p.flight.Unlock()
	var lastErr error
	for {
		p.mu.Lock()
		b := p.current
		if b != nil && !b.dead && time.Since(p.checkedAt) < recheckAfter {
			b.active++
			p.mu.Unlock()
			return b, nil
		}
		p.mu.Unlock()

		pod, ready, err := p.kube.ServicePod(p.ctx, p.target)
		switch {
		case err != nil:
			lastErr = err
		case !ready:
			lastErr = fmt.Errorf("pod %s is not ready", pod)
		default:
			if b != nil && !b.dead && b.pod == pod {
				p.mu.Lock()
				p.checkedAt = time.Now()
				b.active++
				p.mu.Unlock()
				return b, nil
			}
			nb, err := p.start(p.ctx, pod)
			if err == nil {
				p.switchTo(nb)
				return nb, nil
			}
			lastErr = err
		}

		if p.ctx.Err() != nil {
			return nil, p.ctx.Err()
		}
		p.announceWait(lastErr)
		if !time.Now().Before(deadline) {
			p.mu.Lock()
			p.waiting = false
			p.mu.Unlock()
			return nil, lastErr
		}
		timer := time.NewTimer(min(pollInterval, time.Until(deadline)))
		select {
		case <-timer.C:
		case <-p.ctx.Done():
			timer.Stop()
			return nil, p.ctx.Err()
		}
	}
}

// start opens a port-forward to pod on a free internal port and waits until
// it accepts connections.
func (p *tunnelProxy) start(ctx context.Context, pod string) (*backend, error) {
	port, err := FreeLocalPort()
	if err != nil {
		return nil, err
	}
	bctx, cancel := context.WithCancel(p.ctx)
	b := &backend{pod: pod, port: port, cancel: cancel, done: make(chan struct{})}
	p.mu.Lock()
	p.backends[b] = struct{}{}
	p.mu.Unlock()

	readyCh := make(chan struct{})
	var forwardErr error
	go func() {
		forwardErr = p.kube.PortForward(bctx, p.opts, p.target, pod, port, p.status, readyCh)
		p.ended(b, forwardErr)
		close(b.done)
	}()
	select {
	case <-readyCh:
		p.mu.Lock()
		b.ready = true
		p.mu.Unlock()
		return b, nil
	case <-b.done:
		if isClosed(readyCh) {
			// It became ready and ended right away; the caller adopts it and
			// reportIfDied turns that into a reported loss.
			p.mu.Lock()
			b.ready = true
			p.mu.Unlock()
			return b, nil
		}
		if forwardErr == nil {
			forwardErr = errors.New("port-forward stopped before it was ready")
		}
		return nil, forwardErr
	case <-ctx.Done():
		cancel()
		<-b.done
		return nil, ctx.Err()
	}
}

// switchTo makes b the backend for new connections. The previous backend
// keeps serving its open connections and is stopped once they are gone.
func (p *tunnelProxy) switchTo(b *backend) {
	p.mu.Lock()
	old := p.current
	p.current = b
	p.checkedAt = time.Now()
	b.active++
	announce := p.lost || p.waiting || old != nil
	p.lost = false
	p.waiting = false
	var stop *backend
	if old != nil && !old.dead && old.active == 0 {
		stop = old
	}
	p.mu.Unlock()
	if stop != nil {
		stop.cancel()
	}
	if announce {
		_, _ = fmt.Fprintf(p.status, "port-forward to %s re-established via pod %s; new connections go there\n", p.target.ID(), b.pod)
	}
	p.reportIfDied(b)
}

// release ends a connection's use of b and stops b when it is no longer the
// current backend and nothing uses it.
func (p *tunnelProxy) release(b *backend) {
	p.mu.Lock()
	b.active--
	stop := b != p.current && b.active == 0 && !b.dead
	p.mu.Unlock()
	if stop {
		b.cancel()
	}
}

// invalidate forgets b as the current backend, for example after a dial to
// it failed.
func (p *tunnelProxy) invalidate(b *backend) {
	p.mu.Lock()
	b.dead = true
	if p.current == b {
		p.current = nil
	}
	p.mu.Unlock()
}

// ended records that b's port-forward stopped and reports an unexpected loss
// of the current backend.
func (p *tunnelProxy) ended(b *backend, err error) {
	p.mu.Lock()
	delete(p.backends, b)
	b.dead = true
	b.err = err
	wasCurrent := p.current == b
	if wasCurrent {
		p.current = nil
	}
	report := wasCurrent && b.ready && p.ctx.Err() == nil
	if report {
		p.lost = true
	}
	p.mu.Unlock()
	if report {
		p.reportLoss(b, err)
	}
}

// reportIfDied handles a backend whose port-forward ended between becoming
// ready and being adopted as the current backend: ended could not report it
// then, so the loss is reported now.
func (p *tunnelProxy) reportIfDied(b *backend) {
	p.mu.Lock()
	died := b.dead && p.current == b && p.ctx.Err() == nil
	if died {
		p.current = nil
		p.lost = true
	}
	err := b.err
	p.mu.Unlock()
	if died {
		p.reportLoss(b, err)
	}
}

func (p *tunnelProxy) reportLoss(b *backend, err error) {
	reason := "the port-forward ended"
	if err != nil {
		reason = err.Error()
	}
	_, _ = fmt.Fprintf(p.status, "port-forward to %s lost (pod %s): %s; new connections wait up to %s for a ready primary\n", p.target.ID(), b.pod, reason, holdTimeout)
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (p *tunnelProxy) announceWait(cause error) {
	p.mu.Lock()
	first := !p.waiting
	p.waiting = true
	p.mu.Unlock()
	if first {
		_, _ = fmt.Fprintf(p.status, "no ready primary for %s yet (%v); holding new connections for up to %s\n", p.target.ID(), cause, holdTimeout)
	}
}

// splice copies data both ways until either side is done, then closes both.
func splice(a, b net.Conn) {
	var once sync.Once
	closeBoth := func() {
		_ = a.Close()
		_ = b.Close()
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = io.Copy(a, b)
		once.Do(closeBoth)
	})
	wg.Go(func() {
		_, _ = io.Copy(b, a)
		once.Do(closeBoth)
	})
	wg.Wait()
}
