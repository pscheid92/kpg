package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (a *app) connectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "connect [cluster|namespace/cluster|substring] [-- command [args...]]",
		Aliases: []string{"env"},
		Short:   "Connect to a Postgres target",
		Long: `Resolve a Postgres target, start a local foreground port-forward,
and expose PG* environment values for Postgres-compatible clients.

When run interactively without --output or a command, kpg starts a subshell
with PG* environment values exported. Exit that shell to stop the tunnel.

When --output is provided, kpg prints PG* environment values and keeps the
tunnel alive until Ctrl-C.

If a command is provided after --, kpg starts the tunnel, injects PG*
environment values into that command, and stops the tunnel when it exits.`,
		Example: `  kpg connect app-db
  kpg connect
  kpg connect app-db -p 15432
  kpg connect app -o shell
  kpg connect app -o dotenv
  kpg connect app -o json
  kpg connect app-db -- psql
  kpg connect app-db -- pgcli
  kpg connect app-db -u app_owner -d app_db`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a.opts.OutputExplicit = cmd.Root().PersistentFlags().Changed("output")
			target, clientArgs, err := splitConnectArgs(cmd, args)
			if err != nil {
				return err
			}
			if a.opts.OutputExplicit && len(clientArgs) > 0 {
				return fmt.Errorf("--output cannot be combined with a command after --")
			}
			k, err := a.kube()
			if err != nil {
				return err
			}
			return kpg.Connect(commandContext(cmd), a.stdout, a.stderr, k, a.opts, target, clientArgs, true)
		},
		ValidArgsFunction: a.completeConnectTargets,
	}
	cmd.Flags().StringVarP(&a.opts.User, "user", "u", "", "override the Postgres user")
	cmd.Flags().StringVarP(&a.opts.Database, "database", "d", "", "override the Postgres database")
	return cmd
}

func splitConnectArgs(cmd *cobra.Command, args []string) (string, []string, error) {
	return splitConnectArgsAtDash(cmd.ArgsLenAtDash(), args)
}

func splitConnectArgsAtDash(dash int, args []string) (string, []string, error) {
	switch {
	case dash == 0:
		return "", args, nil
	case dash > 0:
		if dash != 1 {
			return "", nil, fmt.Errorf("unexpected arguments before --: %s", strings.Join(args[1:dash], " "))
		}
		return args[0], args[dash:], nil
	case len(args) == 1:
		return args[0], nil, nil
	case len(args) > 1:
		return "", nil, fmt.Errorf("unexpected arguments without --: %s", strings.Join(args[1:], " "))
	default:
		return "", nil, nil
	}
}

func (a *app) completeConnectTargets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	k, err := a.kube()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	targets, err := k.ListTargets(commandContext(cmd), a.opts)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	kpg.SortTargets(targets)
	completions := make([]string, 0, len(targets))
	for _, target := range targets {
		if toComplete != "" && !strings.Contains(target.ID(), toComplete) && !strings.Contains(target.Cluster, toComplete) {
			continue
		}
		completions = append(completions, completionItem(target))
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

func completionItem(target kpg.Target) string {
	var hints []string
	if target.Provider != "" && target.Provider != kpg.ProviderCNPG {
		hints = append(hints, "provider="+target.Provider)
	}
	if target.Database != "" {
		hints = append(hints, "database="+target.Database)
	}
	if target.User != "" {
		hints = append(hints, "user="+target.User)
	}
	if len(hints) == 0 {
		return target.ID()
	}
	return target.ID() + "\t" + strings.Join(hints, " ")
}
