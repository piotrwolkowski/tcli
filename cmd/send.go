package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/piotrwolkowski/tcli/config"
	"github.com/piotrwolkowski/tcli/internal/graph"
	"github.com/spf13/cobra"
)

var sendCmd = &cobra.Command{
	Use:   "send <chat-id-or-alias> <message>",
	Short: "Send a message to a Teams chat",
	Long: `Send a message to a Teams chat. The first argument may be a raw chat ID or
an alias defined via "tcli alias set". The message can be provided as an
argument or piped via stdin.

Examples:
  tcli send 19:abc123@thread.v2 "Hello from the CLI"
  tcli send team-standup "Build passed"
  echo "Build passed" | tcli send team-standup -`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSend,
}

func init() {
	rootCmd.AddCommand(sendCmd)
}

func runSend(cmd *cobra.Command, args []string) error {
	chatID := args[0]
	if aliases, err := config.LoadAliases(); err == nil {
		chatID = aliases.Resolve(chatID)
	}

	var message string
	if len(args) == 2 && args[1] != "-" {
		message = args[1]
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		message = strings.TrimRight(string(data), "\n")
	}

	if message == "" {
		return fmt.Errorf("message cannot be empty")
	}

	client := graph.NewClient()
	resp, err := client.SendMessage(cmd.Context(), chatID, message)
	if err != nil {
		return err
	}

	fmt.Printf("Message sent (id: %s, at: %s)\n", resp.ID, resp.CreatedAt)
	return nil
}
