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
	Use:   "send <chat-id-or-alias> [message]",
	Short: "Send a message or files to a Teams chat",
	Long: `Send a message to a Teams chat. The first argument may be a raw chat ID or
an alias defined via "tcli alias set". The message can be provided as an
argument or piped via stdin.

Examples:
  tcli send 19:abc123@thread.v2 "Hello from the CLI"
  tcli send team-standup "Build passed"
  echo "Build passed" | tcli send team-standup -
  tcli send team-standup --html "<b>Build passed</b><br>all green"
  tcli send team-standup "Q3 report attached" --file report.pdf
  tcli send team-standup -f notes.txt -f data.csv

Plain-text messages have their newlines collapsed by Teams; use --html
(<p>, <br>, <ul>, <pre>, ...) when the message needs structure.

Files given with --file are uploaded to the "Microsoft Teams Chat Files"
folder of your OneDrive, as Teams does, and shared view-only with the other
chat members. With --file the message is optional, and stdin is only read
when the message is "-".`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSend,
}

var (
	sendHTML  bool
	sendFiles []string
)

func init() {
	sendCmd.Flags().BoolVar(&sendHTML, "html", false, "send the message as HTML (contentType html)")
	sendCmd.Flags().StringArrayVarP(&sendFiles, "file", "f", nil, "attach a file (repeatable)")
	rootCmd.AddCommand(sendCmd)
}

func runSend(cmd *cobra.Command, args []string) error {
	chatID, err := resolveChat(args[0])
	if err != nil {
		return err
	}

	message, err := messageText(args[1:], len(sendFiles) > 0)
	if err != nil {
		return err
	}
	if message == "" && len(sendFiles) == 0 {
		return fmt.Errorf("message cannot be empty")
	}
	if err := checkFiles(sendFiles); err != nil {
		return err
	}

	client := graph.NewClient()
	attachments, err := uploadFiles(cmd.Context(), client, chatID, sendFiles)
	if err != nil {
		return err
	}

	resp, err := client.SendMessage(cmd.Context(), chatID, message, sendHTML, attachments...)
	if err != nil {
		return err
	}

	fmt.Printf("Message sent (id: %s, at: %s)\n", resp.ID, resp.CreatedAt)
	return nil
}

// messageText returns the message given in args: the argument itself, or
// stdin when it is "-" or omitted. With files attached an omitted message is
// empty instead, so an open but idle stdin can't block the send.
func messageText(args []string, haveFiles bool) (string, error) {
	if len(args) == 1 && args[0] != "-" {
		return args[0], nil
	}
	if len(args) == 0 && haveFiles {
		return "", nil
	}
	if stdinIsTerminal() {
		return "", fmt.Errorf(`no message given — pass it as an argument, pipe it via stdin (use "-") or attach a file with --file`)
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
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
