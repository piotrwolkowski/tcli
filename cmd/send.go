package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

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
  echo "Build passed" | tcli send team-standup -
  tcli send team-standup --html "<b>Build passed</b><br>all green"

Plain-text messages have their newlines collapsed by Teams; use --html
(<p>, <br>, <ul>, <pre>, ...) when the message needs structure.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSend,
}

var sendHTML bool

func init() {
	sendCmd.Flags().BoolVar(&sendHTML, "html", false, "send the message as HTML (contentType html)")
	rootCmd.AddCommand(sendCmd)
}

func runSend(cmd *cobra.Command, args []string) error {
	chatID, err := resolveChat(args[0])
	if err != nil {
		return err
	}

	var message string
	if len(args) == 2 && args[1] != "-" {
		message = args[1]
	} else {
		if stdinIsTerminal() {
			return fmt.Errorf(`no message given — pass it as an argument or pipe it via stdin (use "-")`)
		}
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
	resp, err := client.SendMessage(cmd.Context(), chatID, message, sendHTML)
	if err != nil {
		return err
	}

	fmt.Printf("Message sent (id: %s, at: %s)\n", resp.ID, resp.CreatedAt)
	return nil
}

// stdinIsTerminal reports whether stdin is an interactive terminal,
// in which case reading it would block waiting for typed input.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
