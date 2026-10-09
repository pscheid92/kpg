package kube

import (
	"context"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pscheid92/kpg/internal/kpg"
)

type zalandoProvider struct{}

var zalandoPostgresqlGVR = schema.GroupVersionResource{
	Group:    "acid.zalan.do",
	Version:  "v1",
	Resource: "postgresqls",
}

func (zalandoProvider) name() string {
	return kpg.ProviderZalando
}

func (zalandoProvider) gvr() schema.GroupVersionResource {
	return zalandoPostgresqlGVR
}

func (zalandoProvider) targets(list unstructured.UnstructuredList) []kpg.Target {
	targets := make([]kpg.Target, 0, len(list.Items))
	for _, item := range list.Items {
		name := item.GetName()
		ns := item.GetNamespace()
		if name == "" || ns == "" {
			continue
		}
		database, user := zalandoDatabaseAndUser(item)
		t := kpg.Target{
			Provider:        kpg.ProviderZalando,
			Namespace:       ns,
			Cluster:         name,
			Database:        database,
			ServiceName:     name,
			DatabaseOptions: zalandoDatabaseOptions(item),
			UserOptions:     zalandoUserOptions(item),
			DatabaseOwners:  zalandoDatabaseOwners(item),
		}
		t = zalandoApplyUser(t, user)
		targets = append(targets, t)
	}
	return targets
}

func (zalandoProvider) enrichTarget(ctx context.Context, c *Client, t kpg.Target) (kpg.Target, error) {
	secrets, err := c.core.CoreV1().Secrets(t.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "application=spilo,cluster-name=" + t.Cluster,
	})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return t, nil
		}
		return t, err
	}
	var users []string
	for _, secret := range secrets.Items {
		if username := string(secret.Data["username"]); username != "" {
			users = append(users, username)
		}
	}
	t.UserOptions = zalandoSortUsers(t.UserOptions, users)
	return t, nil
}

func (p zalandoProvider) resolveConnection(ctx context.Context, c *Client, opts kpg.Options, t kpg.Target) (kpg.Target, kpg.AppSecret, error) {
	return resolveConnectionWith(ctx, c, opts, t, p.applyConnectionOptions)
}

func (zalandoProvider) applyConnectionOptions(t kpg.Target, opts kpg.Options) kpg.Target {
	if opts.Database != "" {
		t.Database = opts.Database
		if opts.User == "" {
			t = zalandoApplyUser(t, t.DatabaseOwners[opts.Database])
		}
	}
	if opts.User != "" {
		t = zalandoApplyUser(t, opts.User)
	}
	return t
}

func zalandoDatabaseOptions(item unstructured.Unstructured) []string {
	var names []string
	if databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases"); found {
		for name := range databases {
			names = append(names, name)
		}
	}
	if prepared, found, _ := unstructured.NestedMap(item.Object, "spec", "preparedDatabases"); found {
		for name := range prepared {
			names = append(names, name)
		}
	}
	return sortedUnique(names)
}

func zalandoUserOptions(item unstructured.Unstructured) []string {
	var names []string
	if users, found := nestedStringSliceMap(item.Object, "spec", "users"); found {
		for name := range users {
			names = append(names, name)
		}
	}
	if databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases"); found {
		for _, owner := range databases {
			names = append(names, owner)
		}
	}
	return zalandoSortUsers(names)
}

func zalandoDatabaseOwners(item unstructured.Unstructured) map[string]string {
	databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases")
	if !found || len(databases) == 0 {
		return nil
	}
	owners := make(map[string]string, len(databases))
	for database, owner := range databases {
		if database != "" && owner != "" {
			owners[database] = owner
		}
	}
	return owners
}

func zalandoDatabaseAndUser(item unstructured.Unstructured) (string, string) {
	databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases")
	if found && len(databases) > 0 {
		var local, crossNamespace []string
		for name, owner := range databases {
			if zalandoIsCrossNamespaceUser(owner) {
				crossNamespace = append(crossNamespace, name)
				continue
			}
			local = append(local, name)
		}
		names := local
		if len(names) == 0 {
			names = crossNamespace
		}
		slices.Sort(names)
		database := names[0]
		return database, databases[database]
	}
	if prepared, found, _ := unstructured.NestedMap(item.Object, "spec", "preparedDatabases"); found && len(prepared) > 0 {
		return slices.Min(slices.Collect(mapKeys(prepared))), ""
	}
	if users, found := nestedStringSliceMap(item.Object, "spec", "users"); found && len(users) > 0 {
		return "", slices.Min(slices.Collect(mapKeys(users)))
	}
	return "", ""
}

func nestedStringSliceMap(obj map[string]any, fields ...string) (map[string][]string, bool) {
	raw, found, _ := unstructured.NestedMap(obj, fields...)
	if !found {
		return nil, false
	}
	result := make(map[string][]string, len(raw))
	for key, value := range raw {
		items, ok := value.([]any)
		if !ok {
			result[key] = nil
			continue
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		result[key] = values
	}
	return result, true
}

func zalandoSecretName(user string, cluster string) string {
	if user == "" {
		return ""
	}
	return strings.ReplaceAll(user, "_", "-") + "." + cluster + ".credentials.postgresql.acid.zalan.do"
}

func zalandoApplyUser(t kpg.Target, user string) kpg.Target {
	if user == "" {
		return t
	}
	secretNamespace, secretUser := zalandoSplitCrossNamespaceUser(user)
	if secretNamespace == "" {
		secretNamespace = t.Namespace
	}
	t.User = secretUser
	t.SecretName = zalandoSecretName(secretUser, t.Cluster)
	t.SecretNamespace = secretNamespace
	return t
}

// zalandoSortUsers merges user lists, drops empty and duplicate names, and
// orders local users before cross-namespace ones so defaults stay predictable.
func zalandoSortUsers(lists ...[]string) []string {
	seen := map[string]struct{}{}
	var local, cross []string
	for _, list := range lists {
		for _, user := range list {
			if user == "" {
				continue
			}
			if _, ok := seen[user]; ok {
				continue
			}
			seen[user] = struct{}{}
			if zalandoIsCrossNamespaceUser(user) {
				cross = append(cross, user)
			} else {
				local = append(local, user)
			}
		}
	}
	slices.Sort(local)
	slices.Sort(cross)
	return append(local, cross...)
}

func zalandoSplitCrossNamespaceUser(user string) (string, string) {
	namespace, name, found := strings.Cut(user, ".")
	if found && namespace != "" && name != "" {
		return namespace, name
	}
	return "", user
}

func zalandoIsCrossNamespaceUser(user string) bool {
	namespace, _ := zalandoSplitCrossNamespaceUser(user)
	return namespace != ""
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	slices.Sort(unique)
	return unique
}

func mapKeys[V any](m map[string]V) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for key := range m {
			if !yield(key) {
				return
			}
		}
	}
}
