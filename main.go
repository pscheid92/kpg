// Command kpg connects to Kubernetes-hosted Postgres databases through a
// local port-forward and exposes the connection as PG* environment values.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/pscheid92/kpg/cmd"
)

func main() {
	if err := cmd.NewRootCommand(os.Stdout, os.Stderr).Execute(); err != nil {
		var exitCoder interface{ ExitCode() int }
		if errors.As(err, &exitCoder) {
			os.Exit(exitCoder.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
