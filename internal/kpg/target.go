package kpg

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func SortTargets(targets []Target) {
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].ID() == targets[j].ID() {
			return targets[i].Provider < targets[j].Provider
		}
		return targets[i].ID() < targets[j].ID()
	})
}

func ResolveTarget(input string, targets []Target) (Target, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Target{}, errors.New("missing target")
	}
	provider, targetText := splitProviderPrefix(input)
	var matches []Target
	for _, t := range targets {
		if provider != "" && t.Provider != provider {
			continue
		}
		id := t.ID()
		switch {
		case strings.Contains(targetText, "/"):
			if id == targetText || strings.Contains(id, targetText) {
				matches = append(matches, t)
			}
		default:
			if t.Cluster == targetText || id == targetText || strings.Contains(t.Cluster, targetText) || strings.Contains(id, targetText) {
				matches = append(matches, t)
			}
		}
	}
	if len(matches) == 0 {
		return Target{}, fmt.Errorf("no target matches %q; try: kpg list", input)
	}
	SortTargets(matches)
	if len(matches) > 1 {
		var b strings.Builder
		_, _ = fmt.Fprintf(&b, "ambiguous target %q; candidates:", targetText)
		for _, t := range matches {
			_, _ = fmt.Fprintf(&b, "\n  %s", t.QualifiedID())
		}
		return Target{}, errors.New(b.String())
	}
	return matches[0], nil
}

func splitProviderPrefix(input string) (string, string) {
	prefix, rest, found := strings.Cut(input, ":")
	if !found || strings.Contains(prefix, "/") || rest == "" {
		return "", input
	}
	return prefix, rest
}

func ApplySecret(t Target, secret AppSecret) Target {
	if secret.Database != "" {
		t.Database = secret.Database
	}
	if secret.Username != "" {
		t.User = secret.Username
	}
	return t
}

func ApplyConnectionOverrides(t Target, opts Options) Target {
	if opts.Database != "" {
		t.Database = opts.Database
	}
	if opts.User != "" {
		t.User = opts.User
		t.SecretName, t.SecretNamespace = providerSecretLocation(t)
	}
	return t
}

func providerSecretLocation(t Target) (string, string) {
	switch t.Provider {
	case ProviderZalando:
		namespace, user := SplitCrossNamespaceUser(t.User)
		if namespace == "" {
			namespace = t.Namespace
		}
		return strings.ReplaceAll(user, "_", "-") + "." + t.Cluster + ".credentials.postgresql.acid.zalan.do", namespace
	default:
		return t.Cluster + "-" + t.User, t.Namespace
	}
}

func SplitCrossNamespaceUser(user string) (string, string) {
	namespace, name, found := strings.Cut(user, ".")
	if found && namespace != "" && name != "" {
		return namespace, name
	}
	return "", user
}

func IsCrossNamespaceUser(user string) bool {
	namespace, _ := SplitCrossNamespaceUser(user)
	return namespace != ""
}
