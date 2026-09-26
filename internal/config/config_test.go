package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Fatalf("cfg = %+v, want defaults", cfg)
	}
}

func TestTemplateIsValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sptui", "config.toml")
	if wrote, err := WriteTemplate(path); err != nil || !wrote {
		t.Fatalf("WriteTemplate = %v, %v", wrote, err)
	}
	if wrote, _ := WriteTemplate(path); wrote {
		t.Fatal("WriteTemplate overwrote an existing file")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("template doesn't parse: %v", err)
	}
	if cfg != Default() {
		t.Fatalf("template changes defaults: %+v", cfg)
	}
}

func TestLoadReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[player]\nenabled = false\nbitrate = 160\n[theme]\naccent = \"#ff00ff\"\n"), 0o600)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Player.Enabled || cfg.Player.Bitrate != 160 || cfg.Player.Name != "sptui" || cfg.Theme.Accent != "#ff00ff" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadRejectsBadBitrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[player]\nbitrate = 128\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for bitrate 128")
	}
}

func TestSaveRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sptui", "config.toml")
	cfg := Default()
	cfg.ClientID = "abc"
	cfg.RedirectURI = DefaultRedirectURI
	cfg.Player.Bitrate = 160
	cfg.Theme.CoverArt = "off"
	cfg.Theme.NowPlayingCover = "small"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, cfg)
	}
}
