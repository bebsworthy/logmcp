//go:build ignore
// +build ignore

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	// Print startup diagnostics
	fmt.Printf("STARTED: stdin_app with args: %v\n", os.Args[1:])
	
	// App that reads from stdin and logs the input
	log.Println("Stdin app started")
	fmt.Println("Ready to receive input. Type 'quit' to exit.")
	fmt.Fprintln(os.Stderr, "Stderr: Waiting for input...")

	scanner := bufio.NewScanner(os.Stdin)
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		input := scanner.Text()
		
		// Log to different streams based on input
		log.Printf("Received input %d: %s", lineCount, input)
		
		// Echo to stdout
		fmt.Printf("Echo: %s\n", input)
		
		// Process commands
		switch strings.ToLower(strings.TrimSpace(input)) {
		case "quit", "exit":
			log.Println("Quit command received, exiting...")
			fmt.Println("Goodbye!")
			os.Exit(0)
		case "error":
			fmt.Fprintln(os.Stderr, "ERROR: Test error message to stderr")
		case "crash":
			log.Println("Crash command received!")
			panic("Intentional crash for testing")
		case "status":
			fmt.Printf("Status: Processed %d lines\n", lineCount)
			fmt.Fprintf(os.Stderr, "Stderr status: Running normally\n")
		default:
			// Convert input based on content
			if strings.HasPrefix(input, "upper:") {
				result := strings.ToUpper(strings.TrimPrefix(input, "upper:"))
				fmt.Printf("Uppercase: %s\n", result)
			} else if strings.HasPrefix(input, "lower:") {
				result := strings.ToLower(strings.TrimPrefix(input, "lower:"))
				fmt.Printf("Lowercase: %s\n", result)
			} else if strings.HasPrefix(input, "repeat:") {
				parts := strings.SplitN(strings.TrimPrefix(input, "repeat:"), ":", 2)
				if len(parts) == 2 {
					count := 3 // default
					fmt.Sscanf(parts[0], "%d", &count)
					for i := 0; i < count; i++ {
						fmt.Printf("Repeat %d: %s\n", i+1, parts[1])
					}
				}
			}
		}
		
		// Always prompt for next input
		fmt.Print("> ")
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading stdin: %v", err)
		fmt.Fprintf(os.Stderr, "Fatal error: %v\n", err)
		os.Exit(1)
	}

	log.Println("Stdin closed, exiting...")
}