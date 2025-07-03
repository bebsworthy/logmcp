package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	// Simple app that logs periodically
	interval := 1 * time.Second
	if len(os.Args) > 1 {
		if d, err := time.ParseDuration(os.Args[1]); err == nil {
			interval = d
		}
	}

	log.Println("Simple app started")
	fmt.Println("Hello from simple app stdout")
	fmt.Fprintln(os.Stderr, "Hello from simple app stderr")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ticker.C:
			count++
			log.Printf("Tick %d", count)
			if count%2 == 0 {
				fmt.Printf("Stdout message %d\n", count)
			} else {
				fmt.Fprintf(os.Stderr, "Stderr message %d\n", count)
			}
		}
	}
}