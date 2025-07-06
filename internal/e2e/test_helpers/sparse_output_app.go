// sparse_output_app.go - Test helper that produces output occasionally
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-sigChan:
			fmt.Println("Received shutdown signal")
			os.Exit(0)
		case <-ticker.C:
			count++
			fmt.Printf("tick %d\n", count)
		}
	}
}