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
		secretNamespace, secretUser := zalandoSecretUser(user)
		if secretNamespace == "" {
			secretNamespace = ns
		}
		t := kpg.Target{
			Provider:        kpg.ProviderZalando,
			Namespace:       ns,
			Cluster:         name,
			Database:        database,
			User:            user,
			ServiceName:     name,
			SecretName:      zalandoSecretName(secretUser, name),
			SecretNamespace: secretNamespace,
		}
		targets = append(targets, t)
	}
	return targets
}

func zalandoDatabaseAndUser(item unstructured.Unstructured) (string, string) {
	databases, found, _ := unstructured.NestedStringMap(item.Object, "spec", "databases")
	if found && len(databases) > 0 {
		names := make([]string, 0, len(databases))
		for name := range databases {
			names = append(names, name)
		}
		sort.Strings(names)
		database := names[0]
		return database, databases[database]
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

func zalandoSecretUser(user string) (string, string) {
	namespace, name, found := strings.Cut(user, ".")
	if found && namespace != "" && name != "" {
		return namespace, name
	}
	return "", user
}

func zalandoSecretName(user string, cluster string) string {
	if user == "" {
		return ""
	}
	return user + "." + cluster + ".credentials.postgresql.acid.zalan.do"
}
