package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	mapv1 "github.com/pmarsceill/mapcli/proto/map/v1"

	"github.com/pmarsceill/mapcli/internal/client"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch real-time events",
	Long:  `Stream events from the daemon in real-time.`,
	RunE:  runWatch,
}

func init() {
	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	c, err := client.New(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	stream, err := c.WatchEvents(ctx)
	if err != nil {
		return fmt.Errorf("watch events: %w", err)
	}

	fmt.Println("watching events (ctrl+c to stop)...")
	fmt.Println()

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled, normal exit
				break
			}
			return fmt.Errorf("receive event: %w", err)
		}

		printEvent(event)
	}

	return nil
}

func printEvent(event *mapv1.Event) {
	ts := event.Timestamp.AsTime().Local().Format("15:04:05")

	// Handle status events (used for agent lifecycle events)
	if se := event.GetStatus(); se != nil && se.Message != "" {
		fmt.Printf("[%s] %s\n", ts, se.Message)
		return
	}

	switch event.Type {
	case mapv1.EventType_EVENT_TYPE_TASK_CREATED:
		if te := event.GetTask(); te != nil {
			fmt.Printf("[%s] task created: %s\n", ts, te.TaskId)
		}

	case mapv1.EventType_EVENT_TYPE_TASK_OFFERED:
		if te := event.GetTask(); te != nil {
			fmt.Printf("[%s] task offered: %s -> %s\n", ts, te.TaskId, te.AgentId)
		}

	case mapv1.EventType_EVENT_TYPE_TASK_ACCEPTED:
		if te := event.GetTask(); te != nil {
			fmt.Printf("[%s] task accepted: %s by %s\n", ts, te.TaskId, te.AgentId)
		}

	case mapv1.EventType_EVENT_TYPE_TASK_STARTED:
		if te := event.GetTask(); te != nil {
			fmt.Printf("[%s] task started: %s\n", ts, te.TaskId)
		}

	case mapv1.EventType_EVENT_TYPE_TASK_COMPLETED:
		if te := event.GetTask(); te != nil {
			fmt.Printf("[%s] task completed: %s\n", ts, te.TaskId)
		}

	case mapv1.EventType_EVENT_TYPE_TASK_FAILED:
		if te := event.GetTask(); te != nil {
			fmt.Printf("[%s] task failed: %s\n", ts, te.TaskId)
		}

	case mapv1.EventType_EVENT_TYPE_TASK_CANCELLED:
		if te := event.GetTask(); te != nil {
			fmt.Printf("[%s] task cancelled: %s\n", ts, te.TaskId)
		}

	default:
		fmt.Printf("[%s] event: %s\n", ts, event.Type.String())
	}
}
