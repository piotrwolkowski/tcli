package cmd

import (
	"fmt"

	"github.com/piotrwolkowski/tcli/internal/auth"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove cached tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.ClearCache(); err != nil {
			return fmt.Errorf("removing token cache: %w", err)
		}
		fmt.Println("Logged out.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
