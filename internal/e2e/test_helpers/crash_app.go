package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	// App that crashes after a delay
	delay := 2 * time.Second
	if len(os.Args) > 1 {
		if d, err := time.ParseDuration(os.Args[1]); err == nil {
			delay = d
		}
	}

	exitCode := 1
	if len(os.Args) > 2 {
		if _, err := fmt.Sscanf(os.Args[2], "%d", &exitCode); err != nil {
			exitCode = 1
		}
	}

	log.Printf("Crash app started, will crash in %v with exit code %d", delay, exitCode)
	fmt.Println("Running normally on stdout")
	fmt.Fprintln(os.Stderr, "Running normally on stderr")

	time.Sleep(delay)

	log.Printf("CRASH! Exiting with code %d", exitCode)
	fmt.Fprintln(os.Stderr, "Fatal error occurred!")
	os.Exit(exitCode)
}