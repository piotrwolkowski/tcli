package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/piotrwolkowski/tcli/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure Azure app credentials (client ID and tenant ID)",
	RunE:  runConfig,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	existing, _ := config.Load()
	if existing == nil {
		existing = &config.Config{}
	}

	reader := bufio.NewReader(os.Stdin)

	clientID, err := prompt(reader, "Client ID", existing.ClientID)
	if err != nil {
		return err
	}
	tenantID, err := prompt(reader, "Tenant ID", existing.TenantID)
	if err != nil {
		return err
	}

	cfg := &config.Config{
		ClientID: clientID,
		TenantID: tenantID,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("Configuration saved.")
	return nil
}

func prompt(reader *bufio.Reader, label, current string) (string, error) {
	if current != "" {
		fmt.Printf("%s [%s]: ", label, current)
	} else {
		fmt.Printf("%s: ", label)
	}

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return current, nil
	}
	return input, nil
}
