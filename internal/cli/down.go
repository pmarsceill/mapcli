package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/spf13/cobra"
)

var forceShutdown bool

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop the mapd daemon",
	Long:  `Stop the mapd daemon process gracefully.`,
	RunE:  runDown,
}

func init() {
	downCmd.Flags().BoolVarP(&forceShutdown, "force", "f", false, "force immediate shutdown")
	rootCmd.AddCommand(downCmd)
}

func runDown(cmd *cobra.Command, args []string) error {
	if !client.IsDaemonRunning(socketPath) {
		fmt.Println("daemon is not running")
		return nil
	}

	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Shutdown(ctx, forceShutdown); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	fmt.Println("daemon stopped")
	return nil
}
