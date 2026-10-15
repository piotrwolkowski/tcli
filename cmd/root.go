package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tcli",
	Short: "Microsoft Teams CLI client",
	Long:  "A command-line client for Microsoft Teams. List chats, send messages, and pipe output — all from your terminal.",
	// Runtime (Graph/auth) errors print only "Error: ..."; arg and flag
	// errors carry the usage line themselves (see withUsage).
	SilenceUsage: true,
}

func init() {
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError(cmd, err)
	})
}

func Execute() error {
	withUsage(rootCmd)
	return rootCmd.Execute()
}

// withUsage wraps the Args validator of cmd and its subcommands so that
// arg-count errors include the command's usage line.
func withUsage(cmd *cobra.Command) {
	if v := cmd.Args; v != nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			if err := v(c, args); err != nil {
				return usageError(c, err)
			}
			return nil
		}
	}
	for _, sub := range cmd.Commands() {
		withUsage(sub)
	}
}

func usageError(cmd *cobra.Command, err error) error {
	return fmt.Errorf("%w\nUsage: %s (see: %s --help)", err, cmd.UseLine(), cmd.CommandPath())
}
