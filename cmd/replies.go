package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/piotrwolkowski/tcli/config"
	"github.com/piotrwolkowski/tcli/internal/graph"
	"github.com/spf13/cobra"
)

var (
	repliesJSON     bool
	repliesMaxPages int
	repliesDownload string
)

var repliesCmd = &cobra.Command{
	Use:   "replies <chat-id-or-alias>",
	Short: "Show messages posted after your most recent message",
	Long: `Show messages in a chat that were posted after your most recent message.
The chat argument may be a raw chat ID or an alias.

If you've never posted in the chat, recent messages are shown instead.

Attached files and inline images are listed under each message. Use
--download <dir> to save them.`,
	Args: cobra.ExactArgs(1),
	RunE: runReplies,
}

func init() {
	repliesCmd.Flags().BoolVar(&repliesJSON, "json", false, "output as JSON")
	repliesCmd.Flags().IntVar(&repliesMaxPages, "max-pages", 5, "maximum message pages to scan when locating your last message")
	repliesCmd.Flags().StringVar(&repliesDownload, "download", "", "save attachments of the shown messages to this directory")
	rootCmd.AddCommand(repliesCmd)
}

func runReplies(cmd *cobra.Command, args []string) error {
	chatID := args[0]
	if aliases, err := config.LoadAliases(); err == nil {
		chatID = aliases.Resolve(chatID)
	}

	client := graph.NewClient()

	me, err := client.Me(cmd.Context())
	if err != nil {
		return fmt.Errorf("fetching current user: %w", err)
	}

	msgs, err := client.ListMessagesAfterMine(cmd.Context(), chatID, me.ID, repliesMaxPages)
	if err != nil {
		return err
	}

	if repliesJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(msgs); err != nil {
			return err
		}
		return downloadAttachments(cmd.Context(), client, chatID, msgs, repliesDownload)
	}

	if len(msgs) == 0 {
		fmt.Println("(no new replies)")
		return nil
	}

	for _, m := range msgs {
		author := "(unknown)"
		if m.From != nil && m.From.User != nil && m.From.User.DisplayName != "" {
			author = m.From.User.DisplayName
		}
		fmt.Printf("[%s] %s:\n%s\n", m.CreatedDateTime, author, renderBody(m.Body))
		for _, a := range m.Attachments {
			if a.IsFile() {
				fmt.Printf("  📎 %s\n", a.Name)
			}
		}
		if n := len(m.HostedContentIDs()); n > 0 {
			fmt.Printf("  🖼  %d inline image(s)\n", n)
		}
		fmt.Println()
	}
	return downloadAttachments(cmd.Context(), client, chatID, msgs, repliesDownload)
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func renderBody(b graph.MessageBodyFull) string {
	content := b.Content
	if strings.EqualFold(b.ContentType, "html") {
		content = htmlTagRe.ReplaceAllString(content, "")
	}
	return strings.TrimSpace(content)
}
