package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (a *app) lastCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "last [-- command [args...]]",
		Short: "Reconnect to the last successful target",
		Long: `Reconnect to the last successful target without storing discovery data or secrets.

Without --output or a command, interactive sessions start a subshell with PG*
environment values exported. A command provided after -- runs with the same PG*
environment values and exits when the command exits.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientArgs, err := lastClientArgs(cmd, args)
			if err != nil {
				return err
			}
			a.opts.OutputExplicit = cmd.Root().PersistentFlags().Changed("output")
			lt, err := kpg.ReadLastTarget()
			if err != nil {
				return fmt.Errorf("no last target: %w", err)
			}
			k, err := a.kube()
			if err != nil {
				return err
			}
			targetText := lt.Namespace + "/" + lt.Cluster
			if lt.Provider != "" {
				targetText = lt.Provider + ":" + targetText
			}
			return kpg.Connect(commandContext(cmd), a.stdout, a.stderr, k, a.opts, targetText, clientArgs, true)
		},
	}
}

func lastClientArgs(cmd *cobra.Command, args []string) ([]string, error) {
	dash := cmd.ArgsLenAtDash()
	switch {
	case dash == 0:
		return args, nil
	case dash > 0:
		return nil, fmt.Errorf("last does not accept a target; use: kpg last -- command")
	case len(args) > 0:
		return nil, fmt.Errorf("last does not accept arguments without --")
	default:
		return nil, nil
	}
}
