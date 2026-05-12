package kube

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pscheid92/kpg/internal/kpg"
)

var zalandoPostgresqlGVR = schema.GroupVersionResource{
	Group:    "acid.zalan.do",
	Version:  "v1",
	Resource: "postgresqls",
}

func (c *Client) targetsFromZalandoList(list unstructured.UnstructuredList) []kpg.Target {
	targets := make([]kpg.Target, 0, len(list.Items))
	for _, item := range list.Items {
		name := item.GetName()
		ns := item.GetNamespace()
		if name == "" || ns == "" {
			continue
		}
		database, user := zalandoDatabaseAndUser(item)
		secretNamespace, secretUser := kpg.SplitCrossNamespaceUser(user)
		if secretNamespace == "" {
			secretNamespace = ns
		}
		t := kpg.Target{
			Provider:        kpg.ProviderZalando,
			Namespace:       ns,
			Cluster:         name,
			Database:        database,
			User:            secretUser,
			ServiceName:     name,
			SecretName:      zalandoSecretName(secretUser, name),
			SecretNamespace: secretNamespace,
			DatabaseOptions: zalandoDatabaseOptions(item),
			UserOptions:     zalandoUserOptions(item),
		}
		targets = append(targets, t)
	}
	return targets
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
		if kpg.IsCrossNamespaceUser(name) {
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

func zalandoDatabaseAndUser(item unstructured.Unstructured) (string, string) {
	databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases")
	if found && len(databases) > 0 {
		var local, crossNamespace []string
		for name, owner := range databases {
			if kpg.IsCrossNamespaceUser(owner) {
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
