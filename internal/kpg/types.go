package kpg

import (
	"context"
	"io"
)

const (
	DefaultOutput  = "shell"
	DefaultSSLMode = "disable"
	StateFileName  = "last.json"

	ProviderCNPG    = "cnpg"
	ProviderZalando = "zalando"
)

type Options struct {
	Context        string
	Namespace      string
	LocalPort      int
	Output         string
	OutputExplicit bool
	Selection      Selection
}

type Selection struct {
	Enabled     bool
	Interactive bool
	In          io.Reader
	Out         io.Writer
}

type Target struct {
	Provider        string
	Namespace       string
	Cluster         string
	Database        string
	User            string
	ServiceName     string
	SecretName      string
	SecretNamespace string
}

type ListTarget struct {
	Target          string `json:"target"`
	QualifiedTarget string `json:"qualified_target,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Namespace       string `json:"namespace"`
	Cluster         string `json:"cluster"`
	Database        string `json:"database,omitempty"`
	User            string `json:"user,omitempty"`
	Service         string `json:"service,omitempty"`
}

func (t Target) ID() string {
	return t.Namespace + "/" + t.Cluster
}

func (t Target) QualifiedID() string {
	if t.Provider == "" {
		return t.ID()
	}
	return t.Provider + ":" + t.ID()
}

type AppSecret struct {
	Username string
	Password string
	Database string
}

type EnvValues struct {
	Host     string `json:"PGHOST"`
	Port     int    `json:"PGPORT"`
	User     string `json:"PGUSER,omitempty"`
	Password string `json:"PGPASSWORD,omitempty"`
	Database string `json:"PGDATABASE,omitempty"`
	SSLMode  string `json:"PGSSLMODE"`
}

type LastTarget struct {
	Provider  string `json:"provider,omitempty"`
	Namespace string `json:"namespace"`
	Cluster   string `json:"cluster"`
}

type Kube interface {
	ListTargets(ctx context.Context, opts Options) ([]Target, error)
	ReadCredentials(ctx context.Context, opts Options, t Target) (AppSecret, bool, error)
	PortForward(ctx context.Context, opts Options, t Target, localPort int, out io.Writer, errOut io.Writer, readyCh chan struct{}) error
}
