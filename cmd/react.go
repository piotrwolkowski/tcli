package cmd

import (
	"fmt"
	"strings"

	"github.com/piotrwolkowski/tcli/internal/graph"
	"github.com/spf13/cobra"
)

var reactRemove bool

var reactCmd = &cobra.Command{
	Use:   "react <chat-id-or-alias> <message-id> <emoji>",
	Short: "React to a message with an emoji",
	Long: `Add an emoji reaction to a message in a Teams chat, or remove it with
--remove. The chat argument may be a raw chat ID or an alias. Message IDs are
shown by "tcli replies" (and its --json output) and printed by "tcli send".

The reaction can be any emoji, or one of the names like, heart, laugh,
surprised, sad and angry.

Examples:
  tcli react standup 1712345678901 👍
  tcli react standup 1712345678901 heart
  tcli react standup 1712345678901 👍 --remove`,
	Args: cobra.ExactArgs(3),
	RunE: runReact,
}

func init() {
	reactCmd.Flags().BoolVar(&reactRemove, "remove", false, "remove the reaction instead of adding it")
	rootCmd.AddCommand(reactCmd)
}

func runReact(cmd *cobra.Command, args []string) error {
	chatID, err := resolveChat(args[0])
	if err != nil {
		return err
	}
	messageID := strings.TrimSpace(args[1])
	reaction := reactionEmoji(args[2])
	if messageID == "" || reaction == "" {
		return fmt.Errorf("message ID and reaction cannot be empty")
	}

	client := graph.NewClient()
	if reactRemove {
		if err := client.UnsetReaction(cmd.Context(), chatID, messageID, reaction); err != nil {
			return err
		}
		fmt.Printf("Removed %s from message %s\n", reaction, messageID)
		return nil
	}
	if err := client.SetReaction(cmd.Context(), chatID, messageID, reaction); err != nil {
		return err
	}
	fmt.Printf("Reacted %s to message %s\n", reaction, messageID)
	return nil
}

// reactionNames maps the classic Teams reaction names to the unicode emoji
// setReaction expects. Graph may also report reactions by these names.
var reactionNames = map[string]string{
	"like":      "👍",
	"heart":     "❤️",
	"laugh":     "😆",
	"surprised": "😮",
	"sad":       "😢",
	"angry":     "😡",
}

// reactionEmoji returns the emoji for a reaction name, or s itself.
func reactionEmoji(s string) string {
	s = strings.TrimSpace(s)
	if e, ok := reactionNames[strings.ToLower(s)]; ok {
		return e
	}
	return s
}
