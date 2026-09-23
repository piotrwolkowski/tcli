package cmd

import (
	"fmt"
	"strings"

	"github.com/piotrwolkowski/tcli/config"
)

// resolveChat maps a chat argument (alias or raw chat ID) to a chat ID.
// A corrupt aliases.json is reported rather than ignored.
func resolveChat(arg string) (string, error) {
	aliases, err := config.LoadAliases()
	if err != nil {
		return "", err
	}
	if id, ok := aliases[arg]; ok {
		return id, nil
	}
	if !looksLikeChatID(arg) {
		return "", fmt.Errorf("unknown alias %q — run: tcli alias list (or pass a chat ID from: tcli chats)", arg)
	}
	return arg, nil
}

// looksLikeChatID reports whether s resembles a Teams chat ID
// (e.g. 19:abc123@thread.v2).
func looksLikeChatID(s string) bool {
	return strings.HasPrefix(s, "19:") && strings.Contains(s, "@")
}
