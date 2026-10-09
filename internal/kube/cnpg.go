package kube

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/pscheid92/kpg/internal/kpg"
)

type cnpgProvider struct{}

var cnpgClusterGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "clusters",
}

const (
	// cnpgDefaultDatabase mirrors the CloudNativePG defaulting webhook: when a
	// bootstrap section leaves the application database unset, the operator
	// creates "app" owned by a role of the same name.
	cnpgDefaultDatabase = "app"
	cnpgSuperuser       = "postgres"
)

func (cnpgProvider) name() string {
	return kpg.ProviderCNPG
}

func (cnpgProvider) gvr() schema.GroupVersionResource {
	return cnpgClusterGVR
}

func (cnpgProvider) targets(list unstructured.UnstructuredList) []kpg.Target {
	targets := make([]kpg.Target, 0, len(list.Items))
	for _, item := range list.Items {
		name := item.GetName()
		ns := item.GetNamespace()
		if name == "" || ns == "" {
			continue
		}
		t := kpg.Target{
			Provider:        kpg.ProviderCNPG,
			Namespace:       ns,
			Cluster:         name,
			ServiceName:     name + "-rw",
			SecretName:      name + "-app",
			SecretNamespace: ns,
			UserSecrets:     map[string]string{},
		}
		if service, found, _ := unstructured.NestedString(item.Object, "status", "writeService"); found && service != "" {
			t.ServiceName = service
		}
		boot := cnpgBootstrapOf(item)
		if boot.secret != "" {
			t.SecretName = boot.secret
		}
		t.Database = boot.database
		t.DatabaseOptions = []string{boot.database}
		t.User = boot.owner
		t.UserOptions = []string{boot.owner}
		t.UserSecrets[boot.owner] = t.SecretName
		for _, role := range cnpgManagedRoles(item) {
			t.UserOptions = appendUnique(t.UserOptions, role.name)
			t.UserSecrets[role.name] = role.secret
		}
		if secret := cnpgSuperuserSecret(item); secret != "" {
			t.UserSecrets[cnpgSuperuser] = secret
		}
		targets = append(targets, t)
	}
	return targets
}

type cnpgBootstrap struct {
	database string
	owner    string
	secret   string
}

// cnpgBootstrapOf reads the application database, its owner, and the
// credentials secret from whichever bootstrap method the cluster uses. All
// three methods carry the same fields and the same defaults.
func cnpgBootstrapOf(item unstructured.Unstructured) cnpgBootstrap {
	var boot cnpgBootstrap
	for _, method := range []string{"initdb", "recovery", "pg_basebackup"} {
		section, found, _ := unstructured.NestedMap(item.Object, "spec", "bootstrap", method)
		if !found {
			continue
		}
		boot.database, _, _ = unstructured.NestedString(section, "database")
		boot.owner, _, _ = unstructured.NestedString(section, "owner")
		boot.secret, _, _ = unstructured.NestedString(section, "secret", "name")
		break
	}
	if boot.database == "" {
		boot.database = cnpgDefaultDatabase
	}
	if boot.owner == "" {
		boot.owner = boot.database
	}
	return boot
}

type cnpgRole struct {
	name   string
	secret string
}

// cnpgManagedRoles returns the declarative roles that can actually be used for
// a connection: present, allowed to log in, and backed by a password secret.
func cnpgManagedRoles(item unstructured.Unstructured) []cnpgRole {
	raw, found, _ := unstructured.NestedSlice(item.Object, "spec", "managed", "roles")
	if !found {
		return nil
	}
	roles := make([]cnpgRole, 0, len(raw))
	for _, entry := range raw {
		role, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(role, "name")
		secret, _, _ := unstructured.NestedString(role, "passwordSecret", "name")
		login, _, _ := unstructured.NestedBool(role, "login")
		ensure, _, _ := unstructured.NestedString(role, "ensure")
		if name == "" || secret == "" || !login || ensure == "absent" {
			continue
		}
		roles = append(roles, cnpgRole{name: name, secret: secret})
	}
	return roles
}

// cnpgSuperuserSecret returns the secret holding the postgres superuser
// password when superuser access is enabled for the cluster.
func cnpgSuperuserSecret(item unstructured.Unstructured) string {
	enabled, found, _ := unstructured.NestedBool(item.Object, "spec", "enableSuperuserAccess")
	if !found || !enabled {
		return ""
	}
	if secret, found, _ := unstructured.NestedString(item.Object, "spec", "superuserSecret", "name"); found && secret != "" {
		return secret
	}
	return item.GetName() + "-superuser"
}

func (cnpgProvider) enrichTarget(_ context.Context, _ *Client, t kpg.Target) (kpg.Target, error) {
	return t, nil
}

func (p cnpgProvider) resolveConnection(ctx context.Context, c *Client, opts kpg.Options, t kpg.Target) (kpg.Target, kpg.AppSecret, error) {
	return resolveConnectionWith(ctx, c, opts, t, p.applyConnectionOptions)
}

func (cnpgProvider) applyConnectionOptions(t kpg.Target, opts kpg.Options) kpg.Target {
	if opts.Database != "" {
		t.Database = opts.Database
	}
	if opts.User != "" {
		t.User = opts.User
		t.SecretName = cnpgUserSecret(t, opts.User)
		t.SecretNamespace = t.Namespace
	}
	return t
}

// cnpgUserSecret maps a user to the secret that stores its password. Known
// users come from the cluster spec; anything else falls back to the
// <cluster>-<user> convention, with the operator's superuser secret for
// postgres.
func cnpgUserSecret(t kpg.Target, user string) string {
	if secret, ok := t.UserSecrets[user]; ok {
		return secret
	}
	if user == cnpgSuperuser {
		return t.Cluster + "-superuser"
	}
	return t.Cluster + "-" + user
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}
