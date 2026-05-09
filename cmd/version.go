package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pscheid92/kpg/internal/buildinfo"
)

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := buildinfo.Current()
			if cmd.Root().PersistentFlags().Changed("output") {
				if a.opts.Output != "json" {
					return fmt.Errorf("version only supports --output json")
				}
				encoder := json.NewEncoder(a.stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(info)
			}
			_, err := fmt.Fprintf(a.stdout, "kpg %s\ncommit: %s\nbuilt: %s\n", info.Version, info.Commit, info.Date)
			return err
		},
	}
}
