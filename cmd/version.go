package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version information (set via ldflags during build)
var (
	BuildDate = "dev"
	GitCommit = "unknown"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print version information about LogMCP.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("LogMCP - Model Context Protocol Log Server\n")
		fmt.Printf("Version:    %s\n", BuildDate)
		fmt.Printf("Git commit: %s\n", GitCommit)
		fmt.Printf("Go version: %s\n", runtime.Version())
		fmt.Printf("OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Printf("\n")
		fmt.Printf("⚠️  This software is 100%% AI-generated. Use at your own risk.\n")
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
