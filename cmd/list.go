package cmd

import (
	"fmt"

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
			if a.opts.OutputExplicit && a.opts.Output != "json" {
				return fmt.Errorf("list supports only --output json; got %q", a.opts.Output)
			}
			k, err := a.kube()
			if err != nil {
				return err
			}
			return kpg.List(commandContext(cmd), a.stdout, a.stderr, k, a.opts)
		},
	}
}
