package kpg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// listResolveWorkers bounds how many credential secrets are read at once.
const listResolveWorkers = 8

func List(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options) error {
	targets, err := kube.ListTargets(ctx, opts)
	if err != nil {
		return fmt.Errorf("list failed: %w", err)
	}
	SortTargets(targets)
	listTargets := resolveListTargets(ctx, kube, opts, targets)
	if len(listTargets) == 0 {
		_, _ = fmt.Fprintln(stderr, "no Postgres targets found"+listScope(opts))
	}
	if opts.Output == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(listTargets)
	}
	return RenderTargetList(stdout, listTargets)
}

// resolveListTargets reads each target's credentials secret so the table shows
// the effective database and user. Secrets are fetched concurrently; a target
// whose secret cannot be read keeps the values from its spec.
func resolveListTargets(ctx context.Context, kube Kube, opts Options, targets []Target) []ListTarget {
	results := make([]ListTarget, len(targets))
	limit := make(chan struct{}, listResolveWorkers)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-limit }()
			if resolved, _, err := kube.ResolveConnection(ctx, opts, t); err == nil {
				t = resolved
			}
			results[i] = NewListTarget(t)
		}()
	}
	wg.Wait()
	return results
}

func listScope(opts Options) string {
	if opts.Namespace != "" {
		return fmt.Sprintf(" in namespace %q", opts.Namespace)
	}
	return "; check the kube context (-c) or restrict the namespace (-n)"
}

func NewListTarget(t Target) ListTarget {
	item := ListTarget{
		Target:    t.ID(),
		Provider:  t.Provider,
		Namespace: t.Namespace,
		Cluster:   t.Cluster,
		Database:  t.Database,
		User:      t.User,
		Service:   serviceName(t),
	}
	if t.Provider != "" {
		item.QualifiedTarget = t.QualifiedID()
	}
	return item
}

func RenderTargetList(w io.Writer, targets []ListTarget) error {
	if len(targets) == 0 {
		return nil
	}
	showProvider := ShouldShowProvider(targets)
	widths := targetListWidthsFor(targets)
	if showProvider {
		if err := writef(w, "%-*s  %-*s  %-*s  %s\n", widths.Target, "TARGET", widths.Provider, "PROVIDER", widths.Database, "DATABASE", "USER"); err != nil {
			return err
		}
	} else if err := writef(w, "%-*s  %-*s  %s\n", widths.Target, "TARGET", widths.Database, "DATABASE", "USER"); err != nil {
		return err
	}
	for _, t := range targets {
		if showProvider {
			if err := writef(w, "%-*s  %-*s  %-*s  %s\n", widths.Target, t.Target, widths.Provider, valueOrDash(t.Provider), widths.Database, valueOrDash(t.Database), valueOrDash(t.User)); err != nil {
				return err
			}
			continue
		}
		if err := writef(w, "%-*s  %-*s  %s\n", widths.Target, t.Target, widths.Database, valueOrDash(t.Database), valueOrDash(t.User)); err != nil {
			return err
		}
	}
	return nil
}

func ShouldShowProvider(targets []ListTarget) bool {
	for _, t := range targets {
		if t.Provider != "" && t.Provider != ProviderCNPG {
			return true
		}
	}
	seen := make(map[string]string, len(targets))
	for _, t := range targets {
		if t.Provider == "" {
			continue
		}
		if provider, ok := seen[t.Target]; ok && provider != t.Provider {
			return true
		}
		seen[t.Target] = t.Provider
	}
	return false
}

func targetListWidthsFor(targets []ListTarget) tableWidths {
	widths := newTableWidths("TARGET", "PROVIDER", "DATABASE", "USER")
	for _, t := range targets {
		widths.fit(t.Target, valueOrDash(t.Provider), valueOrDash(t.Database), valueOrDash(t.User))
	}
	return widths
}
