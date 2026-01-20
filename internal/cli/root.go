package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var socketPath string

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "map",
	Short: "Multi-agent coordination CLI",
	Long:  `map is a CLI for coordinating multiple agents through the mapd daemon.`,
}

// Execute runs the CLI
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", "/tmp/mapd.sock", "daemon socket path")
}
