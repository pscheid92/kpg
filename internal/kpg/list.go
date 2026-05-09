package kpg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

func List(ctx context.Context, stdout io.Writer, stderr io.Writer, kube Kube, opts Options) error {
	targets, err := kube.ListTargets(ctx, opts)
	if err != nil {
		return fmt.Errorf("list failed: %w", err)
	}
	SortTargets(targets)
	listTargets := make([]ListTarget, 0, len(targets))
	for _, t := range targets {
		secret, found, err := kube.ReadCredentials(ctx, opts, t)
		if err == nil && found {
			t = ApplySecret(t, secret)
		}
		listTargets = append(listTargets, NewListTarget(t))
	}
	if opts.Output == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(listTargets)
	}
	return RenderTargetList(stdout, listTargets)
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
	widths := targetListWidthsFor(targets, showProvider)
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

type targetListWidths struct {
	Target   int
	Provider int
	Database int
	User     int
}

func targetListWidthsFor(targets []ListTarget, showProvider bool) targetListWidths {
	widths := targetListWidths{
		Target:   len("TARGET"),
		Provider: len("PROVIDER"),
		Database: len("DATABASE"),
		User:     len("USER"),
	}
	for _, t := range targets {
		widths.Target = max(widths.Target, len(t.Target))
		if showProvider {
			widths.Provider = max(widths.Provider, len(valueOrDash(t.Provider)))
		}
		widths.Database = max(widths.Database, len(valueOrDash(t.Database)))
		widths.User = max(widths.User, len(valueOrDash(t.User)))
	}
	return widths
}
