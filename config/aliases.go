package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Aliases map[string]string

func aliasesPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aliases.json"), nil
}

func LoadAliases() (Aliases, error) {
	path, err := aliasesPath()
	if err != nil {
		return Aliases{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Aliases{}, nil
		}
		return nil, fmt.Errorf("reading aliases: %w", err)
	}

	var aliases Aliases
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, fmt.Errorf("parsing aliases.json: %w", err)
	}
	if aliases == nil {
		aliases = Aliases{}
	}
	return aliases, nil
}

func SaveAliases(aliases Aliases) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	path, err := aliasesPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// SetAlias assigns name → chatID. Since each chat may have at most one
// alias, any existing alias pointing at chatID is removed first.
func (a Aliases) SetAlias(name, chatID string) {
	for existing, id := range a {
		if id == chatID && existing != name {
			delete(a, existing)
		}
	}
	a[name] = chatID
}

// Resolve returns the chat ID for name, or name itself if no alias matches.
func (a Aliases) Resolve(name string) string {
	if id, ok := a[name]; ok {
		return id
	}
	return name
}

// AliasFor returns the alias name for chatID, or "" if none is set.
func (a Aliases) AliasFor(chatID string) string {
	for name, id := range a {
		if id == chatID {
			return name
		}
	}
	return ""
}
