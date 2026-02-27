package cmd

import (
	"github.com/piotrwolkowski/tcli/internal/auth"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Microsoft Teams using device code flow",
	RunE: func(cmd *cobra.Command, args []string) error {
		return auth.Login(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
