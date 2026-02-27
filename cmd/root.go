package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tcli",
	Short: "Microsoft Teams CLI client",
	Long:  "A command-line client for Microsoft Teams. List chats, send messages, and pipe output — all from your terminal.",
}

func Execute() error {
	return rootCmd.Execute()
}
