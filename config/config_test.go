package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points HOME at a temp dir and clears the env overrides.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TCLI_CLIENT_ID", "")
	t.Setenv("TCLI_TENANT_ID", "")
	return home
}

func writeConfigFile(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "tcli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDir(t *testing.T) {
	home := isolate(t)
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if want := filepath.Join(home, ".config", "tcli"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestLoadEnvOnly(t *testing.T) {
	isolate(t)
	t.Setenv("TCLI_CLIENT_ID", "env-c")
	t.Setenv("TCLI_TENANT_ID", "env-t")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientID != "env-c" || cfg.TenantID != "env-t" {
		t.Errorf("got %+v", cfg)
	}
}

func TestLoadFileOnly(t *testing.T) {
	home := isolate(t)
	writeConfigFile(t, home, `{"clientId":"file-c","tenantId":"file-t"}`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientID != "file-c" || cfg.TenantID != "file-t" {
		t.Errorf("got %+v", cfg)
	}
}

func TestLoadEnvOverridesFilePerField(t *testing.T) {
	tests := []struct {
		name                 string
		envClient, envTenant string
		wantClient, wantTen  string
	}{
		{"client from env, tenant from file", "env-c", "", "env-c", "file-t"},
		{"tenant from env, client from file", "", "env-t", "file-c", "env-t"},
		{"both from env", "env-c", "env-t", "env-c", "env-t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := isolate(t)
			writeConfigFile(t, home, `{"clientId":"file-c","tenantId":"file-t"}`)
			t.Setenv("TCLI_CLIENT_ID", tt.envClient)
			t.Setenv("TCLI_TENANT_ID", tt.envTenant)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.ClientID != tt.wantClient || cfg.TenantID != tt.wantTen {
				t.Errorf("got %+v, want client=%q tenant=%q", cfg, tt.wantClient, tt.wantTen)
			}
		})
	}
}

func TestLoadMissingFileNoEnv(t *testing.T) {
	isolate(t)
	cfg, err := Load()
	if err == nil || !strings.Contains(err.Error(), "clientId and tenantId are required") {
		t.Errorf("got cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	home := isolate(t)
	writeConfigFile(t, home, `{not json`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "parsing config.json") {
		t.Errorf("got err=%v", err)
	}
}

func TestLoadPartialFileNoEnv(t *testing.T) {
	for name, content := range map[string]string{
		"client only": `{"clientId":"c"}`,
		"tenant only": `{"tenantId":"t"}`,
		"empty obj":   `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := isolate(t)
			writeConfigFile(t, home, content)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "required") {
				t.Errorf("got err=%v", err)
			}
		})
	}
}

func TestLoadPartialFileCompletedByEnv(t *testing.T) {
	home := isolate(t)
	writeConfigFile(t, home, `{"clientId":"file-c"}`)
	t.Setenv("TCLI_TENANT_ID", "env-t")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientID != "file-c" || cfg.TenantID != "env-t" {
		t.Errorf("got %+v", cfg)
	}
}

func TestLoadUnreadableConfig(t *testing.T) {
	home := isolate(t)
	if err := os.MkdirAll(filepath.Join(home, ".config", "tcli", "config.json"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "reading config") {
		t.Errorf("got err=%v", err)
	}
}

func TestLoadNoHomeValidates(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", "")
	cfg, err := Load()
	if err == nil {
		t.Errorf("Load with no HOME and no env returned nil error and cfg=%+v; want validation error", cfg)
	}
}

func TestLoadNoHomeWithEnv(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", "")
	t.Setenv("TCLI_CLIENT_ID", "c")
	t.Setenv("TCLI_TENANT_ID", "t")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientID != "c" || cfg.TenantID != "t" {
		t.Errorf("got %+v", cfg)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	home := isolate(t)
	if err := Save(&Config{ClientID: "c1", TenantID: "t1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientID != "c1" || cfg.TenantID != "t1" {
		t.Errorf("got %+v", cfg)
	}

	dir := filepath.Join(home, ".config", "tcli")
	fi, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0700 {
		t.Errorf("dir mode = %v, want 0700", di.Mode().Perm())
	}
}

func TestSaveOverwrites(t *testing.T) {
	isolate(t)
	if err := Save(&Config{ClientID: "old-c", TenantID: "old-t"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(&Config{ClientID: "new-c", TenantID: "new-t"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "new-c" || cfg.TenantID != "new-t" {
		t.Errorf("got %+v", cfg)
	}
}

func TestSaveNoHome(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", "")
	if err := Save(&Config{ClientID: "c", TenantID: "t"}); err == nil {
		t.Error("expected error when HOME is unset")
	}
}
