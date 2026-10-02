// Package speaker runs a Spotify Connect device inside sptui, so music plays
// right here on this computer. It wraps go-librespot's daemon: Spotify sees
// sptui as a speaker, and the UI drives it directly, in process (see
// player.go), without going through the Web API. It also watches and
// controls the account's other devices (see cluster.go and remote.go).
package speaker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
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
	cancel  context.CancelFunc
	server  *localServer
	done    chan struct{}
	err     error // why it stopped; read after done is closed
	connect connect
	dj      djResume
	watcher *observer
	meta    trackMeta
	sock    string      // where the speaker takes requests from other sptuis
	client  atomic.Bool // another sptui runs the speaker; requests go there
	chosen  struct {    // see choose
		sync.Mutex
		id string
	}
}

// HasLogin reports whether the speaker has a saved login, so it can start
// without new Credentials.
func HasLogin(statePath string) bool {
	state, err := (&stateStore{path: statePath}).Load()
	return err == nil && len(state.Credentials.Data) > 0
}

// Start launches the speaker in the background. It runs until ctx is
// cancelled or Close is called. If another sptui is already running the
// speaker, this one controls that instead (see instance.go).
func Start(ctx context.Context, cfg Config, creds Credentials, log *slog.Logger) (*Speaker, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Speaker{cancel: cancel, server: newLocalServer(), done: make(chan struct{}), watcher: newObserver(), sock: socketPath(cfg.StatePath)}
	s.dj.path = filepath.Join(filepath.Dir(cfg.StatePath), "dj.json")

	lockPath := cfg.StatePath + ".lock"
	unlock, err := lock(lockPath)
	switch {
	case errors.Is(err, errLocked):
		log.Info("another sptui is running the speaker; controlling it from here")
		s.client.Store(true)
		go s.standBy(ctx, lockPath, cfg, creds, log)
	case err != nil:
		cancel()
		return nil, err
	default:
		if err := s.run(ctx, cfg, creds, unlock, log); err != nil {
			cancel()
			return nil, err
		}
	}
	go s.watch(ctx, log.With("pkg", "observer"))
	return s, nil
}

// run starts the speaker itself. It holds the lock until the speaker stops.
func (s *Speaker) run(ctx context.Context, cfg Config, creds Credentials, unlock func(), log *slog.Logger) error {
	store := &stateStore{path: cfg.StatePath}
	app, err := newApp(cfg, creds, store, s.server, log)
	if err != nil {
		unlock()
		return err
	}
	if err := s.serve(ctx, log); err != nil {
		log.Warn("other sptuis can't share the speaker", "err", err)
	}
	s.client.Store(false)
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
		unlock() // before done, so the next session can run the speaker
		close(s.done)
	}()
	return nil
}

// standBy waits for the sptui running the speaker to quit, then runs it
// here.
func (s *Speaker) standBy(ctx context.Context, lockPath string, cfg Config, creds Credentials, log *slog.Logger) {
	got := make(chan func(), 1)
	go func() {
		unlock, err := lockWait(lockPath)
		if err != nil {
			log.Error("can't wait for the speaker", "err", err)
		}
		got <- unlock
	}()
	select {
	case <-ctx.Done():
		close(s.done)
		// Let the lock go straight away if it comes.
		go func() {
			if unlock := <-got; unlock != nil {
				unlock()
			}
		}()
	case unlock := <-got:
		if unlock == nil {
			<-ctx.Done()
			close(s.done)
			return
		}
		log.Info("the sptui running the speaker quit; running it here")
		if err := s.run(ctx, cfg, creds, unlock, log); err != nil {
			log.Error("speaker stopped", "err", err)
			s.err = err
			close(s.done)
		}
	}
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
