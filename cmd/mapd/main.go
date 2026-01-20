package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pmarsceill/mapcli/internal/daemon"
)

func main() {
	socketPath := flag.String("socket", "/tmp/mapd.sock", "socket path")
	dataDir := flag.String("data-dir", "", "data directory (default ~/.mapd)")
	flag.Parse()

	cfg := &daemon.Config{
		SocketPath: *socketPath,
		DataDir:    *dataDir,
	}

	srv, err := daemon.NewServer(cfg)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nshutting down...")
		srv.Stop()
	}()

	if err := srv.Start(); err != nil {
		log.Fatalf("start server: %v", err)
	}
}
