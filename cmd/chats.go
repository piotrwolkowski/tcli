package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/piotrwolkowski/tcli/internal/graph"
	"github.com/spf13/cobra"
)

var chatsJSON bool

var chatsCmd = &cobra.Command{
	Use:   "chats",
	Short: "List your Teams chats",
	RunE:  runChats,
}

func init() {
	chatsCmd.Flags().BoolVar(&chatsJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(chatsCmd)
}

func runChats(cmd *cobra.Command, args []string) error {
	client := graph.NewClient()
	chats, err := client.ListChats(cmd.Context())
	if err != nil {
		return err
	}

	if chatsJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(chats)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CHAT ID\tTYPE\tNAME")
	for _, chat := range chats {
		name := graph.ChatDisplayName(chat)
		fmt.Fprintf(w, "%s\t%s\t%s\n", chat.ID, chat.ChatType, name)
	}
	return w.Flush()
}
