package main

import (
	"os"

	"github.com/bebsworthy/logmcp/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
