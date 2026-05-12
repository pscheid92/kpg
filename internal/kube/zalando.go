package kube

import (
	"context"
	"sort"
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

func (p zalandoProvider) enrichTarget(ctx context.Context, c *Client, t kpg.Target) (kpg.Target, error) {
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
		username := string(secret.Data["username"])
		if username != "" {
			users = append(users, username)
		}
	}
	t.UserOptions = zalandoMergeUserOptions(t.UserOptions, users)
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
	seen := map[string]struct{}{}
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases"); found {
		for name := range databases {
			add(name)
		}
	}
	if prepared, found, _ := unstructured.NestedMap(item.Object, "spec", "preparedDatabases"); found {
		for name := range prepared {
			add(name)
		}
	}
	sort.Strings(names)
	return names
}

func zalandoUserOptions(item unstructured.Unstructured) []string {
	seen := map[string]struct{}{}
	var local, cross []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		if zalandoIsCrossNamespaceUser(name) {
			cross = append(cross, name)
		} else {
			local = append(local, name)
		}
	}
	if users, found := nestedStringSliceMap(item.Object, "spec", "users"); found {
		for name := range users {
			add(name)
		}
	}
	if databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases"); found {
		for _, owner := range databases {
			add(owner)
		}
	}
	sort.Strings(local)
	sort.Strings(cross)
	return append(local, cross...)
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
		sort.Strings(names)
		database := names[0]
		return database, databases[database]
	}
	if prepared, found, _ := unstructured.NestedMap(item.Object, "spec", "preparedDatabases"); found && len(prepared) > 0 {
		names := make([]string, 0, len(prepared))
		for name := range prepared {
			names = append(names, name)
		}
		sort.Strings(names)
		return names[0], ""
	}
	users, found := nestedStringSliceMap(item.Object, "spec", "users")
	if found && len(users) > 0 {
		names := make([]string, 0, len(users))
		for name := range users {
			names = append(names, name)
		}
		sort.Strings(names)
		return "", names[0]
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

func zalandoMergeUserOptions(a, b []string) []string {
	seen := map[string]struct{}{}
	var local, cross []string
	add := func(user string) {
		if user == "" {
			return
		}
		if _, ok := seen[user]; ok {
			return
		}
		seen[user] = struct{}{}
		if zalandoIsCrossNamespaceUser(user) {
			cross = append(cross, user)
			return
		}
		local = append(local, user)
	}
	for _, user := range a {
		add(user)
	}
	for _, user := range b {
		add(user)
	}
	sort.Strings(local)
	sort.Strings(cross)
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
