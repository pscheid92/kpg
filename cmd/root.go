package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pscheid92/kpg/internal/buildinfo"
	"github.com/pscheid92/kpg/internal/kpg"
	"github.com/pscheid92/kpg/internal/kube"
)

type kubeFactory func(kpg.Options) (kpg.Kube, error)
type contextLister func() ([]string, error)
type namespaceLister func(context.Context, kpg.Options) ([]string, error)

type app struct {
	opts            kpg.Options
	stdout          io.Writer
	stderr          io.Writer
	kubeFactory     kubeFactory
	contextLister   contextLister
	namespaceLister namespaceLister
}

func NewRootCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return newRootCommand(stdout, stderr, func(opts kpg.Options) (kpg.Kube, error) {
		return kube.New(opts)
	})
}

func newRootCommand(stdout io.Writer, stderr io.Writer, factory kubeFactory) *cobra.Command {
	return newRootCommandWithCompleters(stdout, stderr, factory, kube.ContextNames, kube.NamespaceNames)
}

func newRootCommandWithCompleters(stdout io.Writer, stderr io.Writer, factory kubeFactory, contexts contextLister, namespaces namespaceLister) *cobra.Command {
	a := &app{
		opts:            kpg.Options{Output: kpg.DefaultOutput},
		stdout:          stdout,
		stderr:          stderr,
		kubeFactory:     factory,
		contextLister:   contexts,
		namespaceLister: namespaces,
	}
	interactive := isTerminal(os.Stdin) && isTerminal(stdout)
	a.opts.Selection = kpg.Selection{
		Enabled:     interactive,
		Interactive: interactive,
		In:          os.Stdin,
		Out:         stdout,
	}
	root := &cobra.Command{
		Use:           "kpg",
		Short:         "Connect to Kubernetes-hosted Postgres databases",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       buildinfo.Version,
	}
	root.SetVersionTemplate("kpg {{.Version}}\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVarP(&a.opts.Context, "context", "c", "", "kube context override")
	root.PersistentFlags().StringVarP(&a.opts.Namespace, "namespace", "n", "", "restrict discovery/lookup to one namespace")
	root.PersistentFlags().IntVarP(&a.opts.LocalPort, "local-port", "p", 0, "use a fixed local port")
	root.PersistentFlags().StringVarP(&a.opts.Output, "output", "o", kpg.DefaultOutput, "output format: shell, dotenv, or json")
	_ = root.RegisterFlagCompletionFunc("context", a.completeContexts)
	_ = root.RegisterFlagCompletionFunc("namespace", a.completeNamespaces)

	root.AddCommand(a.listCommand())
	root.AddCommand(a.connectCommand())
	root.AddCommand(a.lastCommand())
	root.AddCommand(a.versionCommand())
	return root
}

func isTerminal(value any) bool {
	file, ok := value.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func (a *app) kube() (kpg.Kube, error) {
	if a.opts.Output != "shell" && a.opts.Output != "dotenv" && a.opts.Output != "json" {
		return nil, fmt.Errorf("invalid --output %q: expected shell, dotenv, or json", a.opts.Output)
	}
	if a.opts.LocalPort < 0 || a.opts.LocalPort > 65535 {
		return nil, fmt.Errorf("invalid --local-port %d", a.opts.LocalPort)
	}
	return a.kubeFactory(a.opts)
}

func (a *app) completeContexts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	names, err := a.contextLister()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterCompletions(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeNamespaces(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	names, err := a.namespaceLister(commandContext(cmd), a.opts)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterCompletions(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func filterCompletions(values []string, toComplete string) []string {
	if toComplete == "" {
		return values
	}
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.Contains(value, toComplete) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func commandContext(cmd *cobra.Command) context.Context {
	ctx := cmd.Context()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
