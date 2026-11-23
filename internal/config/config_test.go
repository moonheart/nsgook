package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoreRoundTripAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	st, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore create failed: %v", err)
	}
	got := st.Get()
	want := Default()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("initial config mismatch:\ngot  %+v\nwant %+v", got, want)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file at %s: %v", path, err)
	}

	var notified bool
	st.SetOnChange(func(c Config) {
		if c.Topmost && c.TextFontSize == 18 {
			notified = true
		}
	})
	if err := st.Update(func(c *Config) {
		c.Topmost = true
		c.TextFontSize = 18
		c.VideoVolume = 2.5 // should clamp to 1.0
		c.ExtensionBlocklist = []string{"INSV", ".tmp", "bad ext!"}
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if !notified {
		t.Fatal("expected onChange callback to observe updated config")
	}

	st2, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	reloaded := st2.Get()
	if !reloaded.Topmost || reloaded.TextFontSize != 18 || reloaded.VideoVolume != 1.0 {
		t.Fatalf("unexpected reloaded config: %+v", reloaded)
	}
	if !reflect.DeepEqual(reloaded.ExtensionBlocklist, []string{".insv", ".tmp"}) {
		t.Fatalf("unexpected normalized blocklist: %v", reloaded.ExtensionBlocklist)
	}
}

func TestPartialYAMLPreservesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("close_on_lost_focus: true\ntheme: dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := st.Get()
	if !cfg.CloseOnLostFocus || cfg.Theme != ThemeDark {
		t.Fatalf("partial YAML overrides ignored: %+v", cfg)
	}
	if !cfg.ShowTrayIcon || !cfg.AutoCloseHolding || cfg.TextFontSize != 14 {
		t.Fatalf("defaults were lost on partial YAML: %+v", cfg)
	}
}

func TestExtensionFiltering(t *testing.T) {
	cfg := Default()
	if cfg.IsExtensionAllowed(`C:\clips\video.INSV`) {
		t.Fatal(".insv should be blocked by default blocklist")
	}
	if !cfg.IsExtensionAllowed(`C:\docs\readme.md`) {
		t.Fatal(".md should be allowed in default blocklist mode")
	}
	if !cfg.IsExtensionAllowed(`C:\repo\Makefile`) {
		t.Fatal("extensionless files should always be allowed")
	}
	if !cfg.IsExtensionAllowed(`::{645FF040-5081-101B-9F08-00AA002F954E}`) {
		t.Fatal("CLSID virtual items should always be allowed")
	}

	cfg.UseExtensionAllowlist = true
	if !cfg.IsExtensionAllowed(`C:\docs\notes.txt`) {
		t.Fatal("empty allowlist should allow all files")
	}
	cfg.ExtensionAllowlist = ParseExtensionList("txt; .MD , PNG")
	if !cfg.IsExtensionAllowed(`C:\docs\notes.TXT`) || !cfg.IsExtensionAllowed(`C:\docs\notes.md`) {
		t.Fatal("allowlist should match case-insensitively")
	}
	if cfg.IsExtensionAllowed(`C:\docs\archive.zip`) {
		t.Fatal(".zip should be blocked when not in non-empty allowlist")
	}
}
