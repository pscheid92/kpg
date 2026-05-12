package kube

import (
	"context"
	"encoding/base64"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (c *Client) EnrichTarget(ctx context.Context, t kpg.Target) (kpg.Target, error) {
	provider := c.providerFor(t.Provider)
	if provider == nil {
		return t, nil
	}
	return provider.enrichTarget(ctx, c, t)
}

func (c *Client) ResolveConnection(ctx context.Context, opts kpg.Options, t kpg.Target) (kpg.Target, kpg.AppSecret, error) {
	provider := c.providerFor(t.Provider)
	if provider == nil {
		return resolveConnectionWith(ctx, c, opts, t, kpg.ApplyConnectionOverrides)
	}
	return provider.resolveConnection(ctx, c, opts, t)
}

func resolveConnectionWith(ctx context.Context, c *Client, opts kpg.Options, t kpg.Target, apply func(kpg.Target, kpg.Options) kpg.Target) (kpg.Target, kpg.AppSecret, error) {
	t = apply(t, opts)
	secret, found, err := c.readCredentials(ctx, t)
	if err != nil {
		return kpg.Target{}, kpg.AppSecret{}, err
	}
	if found {
		t = kpg.ApplySecret(t, secret)
		t = apply(t, opts)
	}
	return t, secret, nil
}

func (c *Client) readCredentials(ctx context.Context, t kpg.Target) (kpg.AppSecret, bool, error) {
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
