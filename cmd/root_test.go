package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestUsageError(t *testing.T) {
	root := &cobra.Command{Use: "tcli"}
	sub := &cobra.Command{Use: "send <chat> [message]"}
	root.AddCommand(sub)

	orig := errors.New("accepts between 1 and 2 arg(s), received 0")
	err := usageError(sub, orig)

	for _, want := range []string{
		orig.Error(),
		"Usage: tcli send <chat> [message]",
		"(see: tcli send --help)",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if !errors.Is(err, orig) {
		t.Error("original error not wrapped")
	}
	if !strings.HasPrefix(err.Error(), orig.Error()+"\n") {
		t.Errorf("original error should come first on its own line: %q", err)
	}
}

func TestUsageErrorRootCommand(t *testing.T) {
	root := &cobra.Command{Use: "tcli"}
	err := usageError(root, errors.New("boom"))
	if want := "boom\nUsage: tcli (see: tcli --help)"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}
