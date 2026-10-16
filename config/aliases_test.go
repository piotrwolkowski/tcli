package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAliasesEmptyCases(t *testing.T) {
	tests := []struct {
		name    string
		content string // "" with write=false means no file
		write   bool
	}{
		{"missing file", "", false},
		{"null", "null", true},
		{"empty object", "{}", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := isolate(t)
			if tt.write {
				writeConfigDirFile(t, home, "aliases.json", tt.content)
			}
			a, err := LoadAliases()
			if err != nil {
				t.Fatalf("LoadAliases: %v", err)
			}
			if a == nil {
				t.Fatal("got nil map, want non-nil")
			}
			if len(a) != 0 {
				t.Errorf("got %v, want empty", a)
			}
			a.SetAlias("x", "y") // must not panic on nil map
		})
	}
}

func TestLoadAliasesInvalid(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt":       `{"a":`,
		"array":         `["a","b"]`,
		"non-string":    `{"a":1}`,
		"empty file":    ``,
		"scalar string": `"x"`,
	} {
		t.Run(name, func(t *testing.T) {
			home := isolate(t)
			writeConfigDirFile(t, home, "aliases.json", content)
			if a, err := LoadAliases(); err == nil {
				t.Errorf("expected error, got %v", a)
			}
		})
	}
}

func TestLoadAliasesUnreadable(t *testing.T) {
	home := isolate(t)
	if err := os.MkdirAll(filepath.Join(home, ".config", "tcli", "aliases.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAliases(); err == nil {
		t.Error("expected error for directory named aliases.json")
	}
}

func TestSaveAliasesRoundTrip(t *testing.T) {
	home := isolate(t)
	in := Aliases{"alice": "chat-1", "team": "chat-2"}
	if err := SaveAliases(in); err != nil {
		t.Fatalf("SaveAliases: %v", err)
	}
	out, err := LoadAliases()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out["alice"] != "chat-1" || out["team"] != "chat-2" {
		t.Errorf("got %v", out)
	}
	fi, err := os.Stat(filepath.Join(home, ".config", "tcli", "aliases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSaveAliasesNil(t *testing.T) {
	isolate(t)
	if err := SaveAliases(nil); err != nil {
		t.Fatal(err)
	}
	a, err := LoadAliases()
	if err != nil || a == nil || len(a) != 0 {
		t.Errorf("got %v, %v", a, err)
	}
}

func TestSetAlias(t *testing.T) {
	t.Run("new alias added", func(t *testing.T) {
		a := Aliases{}
		a.SetAlias("n", "c1")
		if a["n"] != "c1" || len(a) != 1 {
			t.Errorf("got %v", a)
		}
	})
	t.Run("re-alias same chat removes old name", func(t *testing.T) {
		a := Aliases{"old": "c1"}
		a.SetAlias("new", "c1")
		if _, ok := a["old"]; ok {
			t.Errorf("old alias still present: %v", a)
		}
		if a["new"] != "c1" || len(a) != 1 {
			t.Errorf("got %v", a)
		}
	})
	t.Run("existing name moves to different chat", func(t *testing.T) {
		a := Aliases{"n": "c1"}
		a.SetAlias("n", "c2")
		if a["n"] != "c2" || len(a) != 1 {
			t.Errorf("got %v", a)
		}
	})
	t.Run("same name same chat idempotent", func(t *testing.T) {
		a := Aliases{"n": "c1"}
		a.SetAlias("n", "c1")
		a.SetAlias("n", "c1")
		if a["n"] != "c1" || len(a) != 1 {
			t.Errorf("got %v", a)
		}
	})
	t.Run("other chats untouched", func(t *testing.T) {
		a := Aliases{"a": "c1", "b": "c2", "c": "c3"}
		a.SetAlias("a2", "c1")
		if a["b"] != "c2" || a["c"] != "c3" || a["a2"] != "c1" || len(a) != 3 {
			t.Errorf("got %v", a)
		}
	})
	t.Run("moving name steals from other chat's alias only by name", func(t *testing.T) {
		a := Aliases{"a": "c1", "b": "c2"}
		a.SetAlias("a", "c2") // a -> c2; b (also c2) must go
		if a["a"] != "c2" || len(a) != 1 {
			t.Errorf("got %v", a)
		}
	})
}

func TestAliasFor(t *testing.T) {
	a := Aliases{"alice": "c1", "bob": "c2"}
	if got := a.AliasFor("c2"); got != "bob" {
		t.Errorf("got %q, want bob", got)
	}
	if got := a.AliasFor("nope"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := (Aliases{}).AliasFor("c1"); got != "" {
		t.Errorf("empty map: got %q", got)
	}
	if got := (Aliases(nil)).AliasFor("c1"); got != "" {
		t.Errorf("nil map: got %q", got)
	}
}
