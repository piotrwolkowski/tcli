package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/piotrwolkowski/tcli/config"
	"github.com/spf13/cobra"
)

var aliasCmd = &cobra.Command{
	Use:   "alias",
	Short: "Manage chat aliases",
	Long:  "Manage local aliases for Teams chat IDs. Aliases are stored in ~/.config/tcli/aliases.json and can be used anywhere a chat ID is accepted.",
}

var aliasSetCmd = &cobra.Command{
	Use:   "set <name> <chat-id>",
	Short: "Create or update a chat alias",
	Args:  cobra.ExactArgs(2),
	RunE:  runAliasSet,
}

var aliasListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all chat aliases",
	Args:  cobra.NoArgs,
	RunE:  runAliasList,
}

var aliasRmCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove", "delete"},
	Short:   "Remove a chat alias",
	Args:    cobra.ExactArgs(1),
	RunE:    runAliasRm,
}

func init() {
	aliasCmd.AddCommand(aliasSetCmd, aliasListCmd, aliasRmCmd)
	rootCmd.AddCommand(aliasCmd)
}

func runAliasSet(cmd *cobra.Command, args []string) error {
	name, chatID := args[0], args[1]

	aliases, err := config.LoadAliases()
	if err != nil {
		return err
	}

	if prev := aliases.AliasFor(chatID); prev != "" && prev != name {
		fmt.Fprintf(os.Stderr, "Replacing existing alias %q for this chat.\n", prev)
	}
	aliases.SetAlias(name, chatID)

	if err := config.SaveAliases(aliases); err != nil {
		return err
	}
	fmt.Printf("Alias %q → %s\n", name, chatID)
	return nil
}

func runAliasList(cmd *cobra.Command, args []string) error {
	aliases, err := config.LoadAliases()
	if err != nil {
		return err
	}
	if len(aliases) == 0 {
		fmt.Println("No aliases set. Use: tcli alias set <name> <chat-id>")
		return nil
	}

	names := make([]string, 0, len(aliases))
	for n := range aliases {
		names = append(names, n)
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ALIAS\tCHAT ID")
	for _, n := range names {
		fmt.Fprintf(w, "%s\t%s\n", n, aliases[n])
	}
	return w.Flush()
}

func runAliasRm(cmd *cobra.Command, args []string) error {
	name := args[0]

	aliases, err := config.LoadAliases()
	if err != nil {
		return err
	}
	if _, ok := aliases[name]; !ok {
		return fmt.Errorf("no alias named %q", name)
	}
	delete(aliases, name)

	if err := config.SaveAliases(aliases); err != nil {
		return err
	}
	fmt.Printf("Removed alias %q\n", name)
	return nil
}
