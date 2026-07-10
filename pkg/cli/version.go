package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the plexus CLI version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, ok := debug.ReadBuildInfo()
			version := "dev"
			commit := "unknown"
			goVersion := "unknown"
			if ok {
				version = info.Main.Version
				goVersion = info.GoVersion
				for _, setting := range info.Settings {
					if setting.Key == "vcs.revision" {
						commit = setting.Value
					}
				}
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "plexus version %s built with Go %s (commit: %s)\n", version, goVersion, commit)
			return err
		},
	}
}
