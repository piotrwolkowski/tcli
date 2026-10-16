package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/piotrwolkowski/tcli/internal/graph"
)

// checkFiles verifies that every path is a non-empty regular file, so a typo
// fails before anything is uploaded.
func checkFiles(paths []string) error {
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		if fi.Size() == 0 {
			return fmt.Errorf("%s is empty — Teams can't attach empty files", p)
		}
	}
	return nil
}

// uploadFiles uploads paths to the user's OneDrive, shares them with the
// other members of chatID and returns the attachments to send. Progress and
// warnings go to stderr.
func uploadFiles(ctx context.Context, client *graph.Client, chatID string, paths []string) ([]graph.Attachment, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	me, err := client.Me(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching current user: %w", err)
	}
	members, err := client.ListChatMembers(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("listing chat members: %w", err)
	}
	others := otherMembers(members, me.ID)

	var attachments []graph.Attachment
	for _, p := range paths {
		a, err := uploadFile(ctx, client, p, others)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, nil
}

func uploadFile(ctx context.Context, client *graph.Client, path string, shareWith []graph.ChatMember) (graph.Attachment, error) {
	f, err := os.Open(path)
	if err != nil {
		return graph.Attachment{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return graph.Attachment{}, err
	}

	item, err := client.UploadChatFile(ctx, filepath.Base(path), f, fi.Size())
	if err != nil {
		return graph.Attachment{}, fmt.Errorf("uploading %s to OneDrive: %w", path, err)
	}
	a, err := item.ChatAttachment()
	if err != nil {
		return graph.Attachment{}, fmt.Errorf("attaching %s: %w", item.Name, err)
	}
	failed, err := client.ShareFile(ctx, item.ID, shareWith)
	if err != nil {
		return graph.Attachment{}, fmt.Errorf("sharing %s with the chat members: %w", item.Name, err)
	}
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "warning: could not share %s with %s — they won't be able to open it\n", item.Name, memberNames(failed))
	}
	fmt.Fprintf(os.Stderr, "uploaded %s\n", item.Name)
	return a, nil
}

// otherMembers returns members without the user meID.
func otherMembers(members []graph.ChatMember, meID string) []graph.ChatMember {
	var others []graph.ChatMember
	for _, m := range members {
		if m.UserID != meID {
			others = append(others, m)
		}
	}
	return others
}

func memberNames(members []graph.ChatMember) string {
	names := make([]string, len(members))
	for i, m := range members {
		switch {
		case m.DisplayName != "":
			names[i] = m.DisplayName
		case m.Email != "":
			names[i] = m.Email
		default:
			names[i] = "(unnamed member)"
		}
	}
	return strings.Join(names, ", ")
}
