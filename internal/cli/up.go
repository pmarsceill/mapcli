package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/pmarsceill/mapcli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	foreground bool
	dataDir    string
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the mapd daemon",
	Long:  `Start the mapd daemon process. By default runs in the background.`,
	RunE:  runUp,
}

func init() {
	upCmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "run in foreground")
	upCmd.Flags().StringVarP(&dataDir, "data-dir", "d", "", "data directory (default ~/.mapd)")
	rootCmd.AddCommand(upCmd)
}

func runUp(cmd *cobra.Command, args []string) error {
	// Check if already running
	if client.IsDaemonRunning(getSocketPath()) {
		fmt.Println("daemon is already running")
		return nil
	}

	if foreground {
		return runForeground()
	}

	return runBackground()
}

func runForeground() error {
	cfg := &daemon.Config{
		SocketPath: getSocketPath(),
		DataDir:    dataDir,
	}

	srv, err := daemon.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nshutting down...")
		srv.Stop()
	}()

	fmt.Printf("starting mapd (foreground)...\n")
	return srv.Start()
}

func runBackground() error {
	// Start daemon as background process
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}

	args := []string{"up", "-f", "-s", getSocketPath()}
	if dataDir != "" {
		args = append(args, "-d", dataDir)
	}

	proc := exec.Command(executable, args...)
	proc.Stdout = nil
	proc.Stderr = nil
	proc.Stdin = nil

	// Detach from parent
	proc.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := proc.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Printf("mapd started (pid %d)\n", proc.Process.Pid)
	return nil
}
