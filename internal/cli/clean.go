package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pmarsceill/mapcli/internal/daemon"
	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up orphaned processes and resources",
	Long: `Clean up orphaned mapd processes, tmux sessions, and socket files.

This is useful when the daemon didn't shut down cleanly and left behind
stale processes or socket files that prevent starting a new daemon.`,
	RunE: runClean,
}

func init() {
	rootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) error {
	var cleaned bool

	// 1. Kill orphaned mapd/map processes
	killedProcs, err := killOrphanedProcesses()
	if err != nil {
		fmt.Printf("warning: error killing processes: %v\n", err)
	}
	if killedProcs > 0 {
		fmt.Printf("killed %d orphaned process(es)\n", killedProcs)
		cleaned = true
	}

	// 2. Kill orphaned tmux sessions
	killedSessions, err := killOrphanedTmuxSessions()
	if err != nil {
		fmt.Printf("warning: error killing tmux sessions: %v\n", err)
	}
	if killedSessions > 0 {
		fmt.Printf("killed %d orphaned tmux session(s)\n", killedSessions)
		cleaned = true
	}

	// 3. Remove socket file if it exists
	if _, err := os.Stat(getSocketPath()); err == nil {
		if err := os.Remove(getSocketPath()); err != nil {
			fmt.Printf("warning: failed to remove socket %s: %v\n", getSocketPath(), err)
		} else {
			fmt.Printf("removed socket %s\n", getSocketPath())
			cleaned = true
		}
	}

	if !cleaned {
		fmt.Println("nothing to clean")
	}

	return nil
}

// killOrphanedProcesses finds and kills mapd and map processes
func killOrphanedProcesses() (int, error) {
	// Get current process ID to avoid killing ourselves
	currentPID := os.Getpid()

	// Find mapd and map processes using pgrep
	output, err := exec.Command("pgrep", "-f", "mapd|map up").Output()
	if err != nil {
		// pgrep returns exit code 1 when no processes found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return 0, nil
		}
		return 0, err
	}

	var killed int
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		// Don't kill ourselves
		if pid == currentPID {
			continue
		}
		// Kill the process
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if err := proc.Kill(); err == nil {
			killed++
		}
	}

	return killed, nil
}

// killOrphanedTmuxSessions kills map-agent-* tmux sessions
func killOrphanedTmuxSessions() (int, error) {
	sessions, err := daemon.ListTmuxSessions()
	if err != nil {
		return 0, err
	}

	var killed int
	for _, session := range sessions {
		cmd := exec.Command("tmux", "kill-session", "-t", session)
		if err := cmd.Run(); err == nil {
			killed++
		}
	}

	return killed, nil
}
