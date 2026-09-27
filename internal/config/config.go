// Package config loads sptui's settings from a TOML file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kyledickey/sptui/internal/atomicfile"
)

// Config is everything a user can configure. Every setting is optional.
type Config struct {
	// ClientID is your own Spotify app, for a Web API rate limit that isn't
	// shared with other apps. Its RedirectURI must be registered in the app.
	ClientID    string `toml:"client_id"`
	RedirectURI string `toml:"redirect_uri"`

	Player Player `toml:"player"`
	Theme  Theme  `toml:"theme"`
}

// DefaultRedirectURI is used with ClientID when RedirectURI isn't set.
const DefaultRedirectURI = "http://127.0.0.1:8989/login"

// Player configures the built-in speaker that plays audio in sptui.
type Player struct {
	// Enabled plays music in sptui itself. When false, sptui only controls
	// other Spotify devices (your phone, a speaker, the desktop app).
	Enabled bool `toml:"enabled"`
	// Name is how sptui shows up in Spotify's device list.
	Name string `toml:"name"`
	// Backend is the audio system: "pulseaudio", "alsa", "audio-toolbox"
	// (macOS) or "wasapi" (Windows). Empty picks one automatically.
	Backend string `toml:"backend"`
	// Bitrate is the streaming quality in kbps: 96, 160 or 320.
	Bitrate int `toml:"bitrate"`
	// KeepAwake stops the computer sleeping and the screen turning off:
	// "playing" (while music plays), "always" (while sptui is open) or "off".
	KeepAwake string `toml:"keep_awake"`
}

// Theme holds appearance settings.
type Theme struct {
	// Accent is a hex color like "#1ed760", or "cover" to take it from
	// the playing song's cover art.
	Accent string `toml:"accent"`
	// ScrollTitles scrolls titles too long to fit, instead of cutting them.
	ScrollTitles bool `toml:"scroll_titles"`
	// CoverArt is "auto", "kitty" (real images), "blocks" (pixel style) or
	// "off". Auto uses kitty in terminals known to support it.
	CoverArt string `toml:"cover_art"`
	// NowPlayingCover sizes the track panel (and so the cover) in the
	// now-playing view: "small", "medium" or "large".
	NowPlayingCover string `toml:"now_playing_cover"`
	// NowPlayingPanels lists the now-playing panels shown: 1 track,
	// 2 lyrics, 3 up next. Pressing a number there toggles it.
	NowPlayingPanels string `toml:"now_playing_panels"`
	// Intro names the short animation played when sptui starts, or "off".
	Intro string `toml:"intro"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Player: Player{Enabled: true, Name: "sptui", Bitrate: 320, KeepAwake: "playing"},
		Theme:  Theme{CoverArt: "auto", ScrollTitles: true, NowPlayingCover: "medium", NowPlayingPanels: "123", Intro: "vinyl"},
	}
}

// Dir returns sptui's config directory, e.g. ~/.config/sptui.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sptui"), nil
}

// CacheDir returns where sptui keeps its login and logs, e.g. ~/.cache/sptui.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sptui"), nil
}

// Load reads the config file at path, using defaults for anything it doesn't
// set, or entirely when the file doesn't exist.
func Load(path string) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	switch cfg.Player.Bitrate {
	case 96, 160, 320:
	default:
		return cfg, fmt.Errorf("config %s: player.bitrate must be 96, 160 or 320", path)
	}
	if cfg.Player.Name == "" {
		cfg.Player.Name = Default().Player.Name
	}
	switch cfg.Player.KeepAwake {
	case "playing", "always", "off":
	default:
		return cfg, fmt.Errorf("config %s: player.keep_awake must be playing, always or off", path)
	}
	switch cfg.Theme.NowPlayingCover {
	case "small", "medium", "large":
	default:
		return cfg, fmt.Errorf("config %s: theme.now_playing_cover must be small, medium or large", path)
	}
	if cfg.Theme.NowPlayingPanels == "" || strings.Trim(cfg.Theme.NowPlayingPanels, "123") != "" {
		return cfg, fmt.Errorf("config %s: theme.now_playing_panels must be some of 1, 2 and 3, like \"123\"", path)
	}
	if cfg.ClientID != "" && cfg.RedirectURI == "" {
		cfg.RedirectURI = DefaultRedirectURI
	}
	return cfg, nil
}

// Save writes cfg to path, replacing what was there.
func Save(path string, cfg Config) error {
	var b bytes.Buffer
	b.WriteString("# sptui configuration. Change it here or in sptui's settings (press ,).\n\n")
	if err := toml.NewEncoder(&b).Encode(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, b.Bytes(), 0o644)
}

// WriteTemplate creates a commented config file at path if none exists.
// It reports whether a file was written.
func WriteTemplate(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(template), 0o644)
}

const template = `# sptui configuration. Everything here is optional.

# Browsing and search go through a Spotify app whose rate limit is shared
# with other apps. If you see "rate limited" a lot, create your own app at
# https://developer.spotify.com/dashboard, add the redirect URI below to it,
# and put its Client ID here.
# client_id = ""
# redirect_uri = "http://127.0.0.1:8989/login"

[player]
# Play music in sptui itself. Set to false to only control other devices.
# enabled = true

# How sptui appears in Spotify's device list.
# name = "sptui"

# Audio system: "pulseaudio" (also PipeWire), "alsa", "audio-toolbox" (macOS)
# or "wasapi" (Windows). Leave unset to pick automatically.
# backend = ""

# Streaming quality in kbps: 96, 160 or 320.
# bitrate = 320

# Keep the computer and screen awake: "playing" (while music plays),
# "always" (while sptui is open) or "off".
# keep_awake = "playing"

[theme]
# A hex color, or "cover" to follow the playing song's cover art.
# accent = "#1ed760"

# Scroll song titles that are too long to fit.
# scroll_titles = true

# Album art: "auto", "kitty" (real images in kitty, Ghostty, and tmux with
# allow-passthrough), "blocks" (pixel style, any terminal) or "off".
# cover_art = "auto"

# Size of the track panel (and its cover) in the now-playing view:
# "small", "medium" or "large".
# now_playing_cover = "medium"

# Now-playing panels to show: 1 track, 2 lyrics, 3 up next. Press the
# numbers in the view to toggle them.
# now_playing_panels = "123"

# The short animation played when sptui starts, or "off". See the list in
# sptui's settings (press ,).
# intro = "vinyl"
`
