package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version is set via -ldflags at build time
var Version = "dev"

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:     "map",
	Short:   "Multi-agent coordination CLI",
	Long:    `map is a CLI for coordinating multiple agents through the mapd daemon.`,
	Version: Version,
}

// Execute runs the CLI
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// getSocketPath returns the socket path from Viper (flag > env > config > default)
func getSocketPath() string {
	return viper.GetString("socket")
}

// getMultiplexer returns the multiplexer type from Viper (env > config > default)
// Returns "tmux" or "zellij"
func getMultiplexer() string {
	return viper.GetString("multiplexer")
}

func init() {
	rootCmd.PersistentFlags().StringP("socket", "s", "/tmp/mapd.sock", "daemon socket path")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.mapd/config.yaml)")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return initConfig()
	}
}
