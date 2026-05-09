package cmd

import (
	"github.com/spf13/cobra"

	"github.com/pscheid92/kpg/internal/kpg"
)

func (a *app) listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List Postgres targets",
		Long:  "List discoverable Postgres targets across the active kube context.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a.opts.OutputExplicit = cmd.Root().PersistentFlags().Changed("output")
			k, err := a.kube()
			if err != nil {
				return err
			}
			return kpg.List(commandContext(cmd), a.stdout, a.stderr, k, a.opts)
		},
	}
}
