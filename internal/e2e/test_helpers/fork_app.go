package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

func main() {
	// App that creates child processes
	numChildren := 2
	if len(os.Args) > 1 {
		if _, err := fmt.Sscanf(os.Args[1], "%d", &numChildren); err == nil && numChildren > 0 && numChildren <= 10 {
			// Limit to 10 children for safety
		} else {
			numChildren = 2
		}
	}

	log.Printf("Fork app started, creating %d child processes", numChildren)
	fmt.Println("Parent process started")

	var wg sync.WaitGroup
	childProcesses := make([]*exec.Cmd, numChildren)

	// Start child processes
	for i := 0; i < numChildren; i++ {
		wg.Add(1)
		childNum := i + 1

		// Create a simple child process that runs for a while
		cmd := exec.Command("sh", "-c", fmt.Sprintf(`
			echo "Child %d started with PID $$"
			for i in 1 2 3 4 5; do
				echo "Child %d: iteration $i"
				sleep 1
			done
			echo "Child %d exiting"
		`, childNum, childNum, childNum))

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start child %d: %v", childNum, err)
			wg.Done()
			continue
		}

		childProcesses[i] = cmd
		log.Printf("Started child %d with PID %d", childNum, cmd.Process.Pid)

		// Monitor child process
		go func(childNum int, cmd *exec.Cmd) {
			defer wg.Done()
			if err := cmd.Wait(); err != nil {
				log.Printf("Child %d exited with error: %v", childNum, err)
			} else {
				log.Printf("Child %d exited successfully", childNum)
			}
		}(childNum, cmd)
	}

	// Parent continues to run and log
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	go func() {
		count := 0
		for range ticker.C {
			count++
			log.Printf("Parent tick %d", count)
			fmt.Printf("Parent stdout message %d\n", count)
		}
	}()

	// Wait for all children to complete
	wg.Wait()
	log.Println("All children have exited, parent continuing...")

	// Keep parent running for a bit more
	time.Sleep(5 * time.Second)
	log.Println("Parent exiting")
}