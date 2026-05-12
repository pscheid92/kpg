package kube

import (
	"context"
	"encoding/base64"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (c *Client) ListClusterUsers(ctx context.Context, t kpg.Target) ([]string, error) {
	if t.Provider != kpg.ProviderZalando {
		return nil, nil
	}
	secrets, err := c.core.CoreV1().Secrets(t.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "application=spilo,cluster-name=" + t.Cluster,
	})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return nil, nil
		}
		return nil, err
	}
	seen := map[string]struct{}{}
	var users []string
	for _, secret := range secrets.Items {
		username := string(secret.Data["username"])
		if username == "" {
			continue
		}
		if _, ok := seen[username]; ok {
			continue
		}
		seen[username] = struct{}{}
		users = append(users, username)
	}
	return users, nil
}

func (c *Client) ReadCredentials(ctx context.Context, opts kpg.Options, t kpg.Target) (kpg.AppSecret, bool, error) {
	secretNamespace := firstNonEmpty(t.SecretNamespace, t.Namespace)
	secretName := t.SecretName
	if secretName == "" {
		secretName = t.Cluster + "-app"
	}
	secret, err := c.core.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return kpg.AppSecret{}, false, nil
		}
		return kpg.AppSecret{}, false, err
	}
	return kpg.AppSecret{
		Username: firstNonEmpty(secretString(secret.Data, "username"), t.User),
		Password: secretString(secret.Data, "password"),
		Database: firstNonEmpty(
			secretString(secret.Data, "dbname"),
			secretString(secret.Data, "database"),
			t.Database,
		),
	}, true, nil
}

func secretString(data map[string][]byte, key string) string {
	if data == nil {
		return ""
	}
	return string(data[key])
}

func DecodeLegacySecretValue(data map[string]string, key string) string {
	raw := data[key]
	if raw == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
