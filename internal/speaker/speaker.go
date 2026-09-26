// Package speaker runs a Spotify Connect device inside sptui, so music plays
// right here on this computer. It wraps go-librespot's daemon: Spotify sees
// sptui as a speaker, and the UI drives it directly, in process (see
// player.go), without going through the Web API.
package speaker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/devgianlu/go-librespot/daemon"
	"github.com/devgianlu/go-librespot/mpris"
)

// Config describes the speaker.
type Config struct {
	Name      string // device name shown in Spotify, e.g. "sptui"
	Backend   string // audio backend; "" picks one for this system
	Bitrate   int    // 96, 160 or 320 kbps
	StatePath string // where the device ID and saved login live
}

// Credentials log the speaker in the first time. Afterwards it uses the
// login it saved in Config.StatePath, and these can be empty (see HasLogin).
type Credentials struct {
	Username    string // optional; Spotify works it out from the token
	AccessToken string // from auth.Streaming
}

// Speaker is a running Spotify Connect device.
type Speaker struct {
	cancel context.CancelFunc
	server *localServer
	done   chan struct{}
	err    error // why it stopped; read after done is closed
}

// HasLogin reports whether the speaker has a saved login, so it can start
// without new Credentials.
func HasLogin(statePath string) bool {
	state, err := (&stateStore{path: statePath}).Load()
	return err == nil && len(state.Credentials.Data) > 0
}

// Start launches the speaker in the background. It runs until ctx is
// cancelled or Close is called.
func Start(ctx context.Context, cfg Config, creds Credentials, log *slog.Logger) (*Speaker, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Speaker{cancel: cancel, server: newLocalServer(), done: make(chan struct{})}
	store := &stateStore{path: cfg.StatePath}

	app, err := newApp(cfg, creds, store, s.server, log)
	if err != nil {
		cancel()
		return nil, err
	}
	log.Info("speaker starting", "name", cfg.Name, "backend", backendOrDefault(cfg.Backend), "bitrate", cfg.Bitrate)

	go func() {
		start := time.Now()
		err := app.Run(ctx)
		// A saved login that no longer works fails straight away. Forget it,
		// and log in again with the fresh token if there is one.
		if err != nil && ctx.Err() == nil && time.Since(start) < 30*time.Second && hadLogin(store) {
			if creds.AccessToken == "" {
				err = fmt.Errorf("its saved login no longer works; restart sptui to log in again: %w", err)
			} else {
				log.Warn("speaker's saved login failed, retrying with a new one", "err", err)
				if app, err = newApp(cfg, creds, store, s.server, log); err == nil {
					err = app.Run(ctx)
				}
			}
		}
		if ctx.Err() != nil {
			err = nil // stopped on purpose
		}
		if err != nil {
			log.Error("speaker stopped", "err", err)
		}
		s.err = err
		close(s.done)
	}()
	return s, nil
}

// Done is closed when the speaker stops.
func (s *Speaker) Done() <-chan struct{} { return s.done }

// Err says why the speaker stopped: nil if it was closed on purpose. Only
// valid once Done is closed.
func (s *Speaker) Err() error { return s.err }

// Close stops the speaker and waits (briefly) for it to finish, so its
// saved state can be deleted safely afterwards.
func (s *Speaker) Close() {
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}
}

// Forget deletes the speaker's saved login. Its device ID is kept, so
// Spotify's device list doesn't collect a new "sptui" each time.
func Forget(statePath string) error {
	_, err := (&stateStore{path: statePath}).forgetLogin()
	return err
}

func newApp(cfg Config, creds Credentials, store *stateStore, server *localServer, log *slog.Logger) (*daemon.App, error) {
	llog := &logger{log: log.With("pkg", "librespot")}
	routeLogrus(llog.log)

	dc := &daemon.Config{
		DeviceName:       cfg.Name,
		DeviceType:       "computer",
		AudioBackend:     backendOrDefault(cfg.Backend),
		AudioDevice:      "default",
		MixerControlName: "Master",
		Bitrate:          cfg.Bitrate,
		VolumeSteps:      100,
		InitialVolume:    50,
		SkipDebounce:     600 * time.Millisecond,
	}
	dc.Credentials.Type = "spotify_token"
	dc.Credentials.SpotifyToken.Username = creds.Username
	dc.Credentials.SpotifyToken.AccessToken = creds.AccessToken

	opts := &daemon.Options{Logger: llog, Config: dc, StateStore: store, APIServer: server}
	// Media keys and desktop widgets via MPRIS, where the platform has it.
	if runtime.GOOS == "linux" {
		if srv, err := mpris.NewServer(llog); err != nil {
			log.Debug("media keys unavailable", "err", err)
		} else {
			opts.MediaPlayer = srv
		}
	}
	app, err := daemon.New(opts)
	if err != nil {
		return nil, fmt.Errorf("start speaker: %w", err)
	}
	return app, nil
}

// hadLogin forgets the saved login, reporting whether there was one.
func hadLogin(store *stateStore) bool {
	forgot, err := store.forgetLogin()
	return forgot && err == nil
}

// backendOrDefault picks the audio backend: PulseAudio (which PipeWire also
// provides) on Linux when it's running, else ALSA; the native one elsewhere.
func backendOrDefault(backend string) string {
	if backend != "" {
		return backend
	}
	switch runtime.GOOS {
	case "darwin":
		return "audio-toolbox"
	case "windows":
		return "wasapi"
	}
	if os.Getenv("PULSE_SERVER") != "" {
		return "pulseaudio"
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "pulse", "native")); err == nil {
			return "pulseaudio"
		}
	}
	return "alsa"
}
