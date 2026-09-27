// Command sptui is a Spotify client for the terminal.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/auth"
	"github.com/kyledickey/sptui/internal/awake"
	"github.com/kyledickey/sptui/internal/cache"
	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/demo"
	"github.com/kyledickey/sptui/internal/logging"
	"github.com/kyledickey/sptui/internal/lyrics"
	"github.com/kyledickey/sptui/internal/speaker"
	"github.com/kyledickey/sptui/internal/spotify"
	"github.com/kyledickey/sptui/internal/tui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// userAgent identifies sptui to services that ask for it (LRCLIB).
var userAgent = "sptui/" + version + " (https://github.com/kyledickey/sptui)"

const usage = `sptui — Spotify in your terminal

Usage:
  sptui [flags]          start the player
  sptui login            log in to Spotify (again)
  sptui logout           forget the saved login

Flags:
`

func main() {
	err := run(os.Args[1:])
	switch {
	case errors.Is(err, flag.ErrHelp):
		return
	case err != nil:
		fmt.Fprintln(os.Stderr, "sptui:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	configDir, err := config.Dir()
	if err != nil {
		return err
	}
	cacheDir, err := config.CacheDir()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("sptui", flag.ContinueOnError)
	configPath := fs.String("config", filepath.Join(configDir, "config.toml"), "config file")
	logPath := fs.String("log", filepath.Join(cacheDir, "sptui.log"), "log file")
	debug := fs.Bool("debug", false, "include debug messages in the log")
	demoMode := fs.Bool("demo", false, "run with made-up music; no Spotify account needed")
	showVersion := fs.Bool("version", false, "print the version")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("sptui", version)
		return nil
	}

	log, closeLog, err := logging.Open(*logPath, *debug)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer closeLog()
	log.Info("starting", "version", version, "demo", *demoMode, "config", *configPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if _, err := config.WriteTemplate(*configPath); err != nil {
		log.Warn("couldn't write config template", "err", err)
	}
	keeper := awake.New("sptui", log.With("pkg", "awake"))
	defer keeper.Close()

	a := &app{
		keeper:       keeper,
		demo:         *demoMode,
		configPath:   *configPath,
		tokenPath:    filepath.Join(cacheDir, "token.json"),
		speakerState: filepath.Join(cacheDir, "speaker.json"),
		libraryCache: filepath.Join(cacheDir, "library"),
		log:          log,
	}
	if err := a.configure(); err != nil {
		return err
	}

	switch cmd := fs.Arg(0); cmd {
	case "":
	case "logout":
		return a.logout()
	case "login":
		// Forget the old login; starting up logs in again.
		if err := a.logout(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command %q (try sptui -h)", cmd)
	}

	for {
		outcome, err := a.session(ctx)
		if err != nil {
			return err
		}
		switch outcome {
		case tui.Quit:
			log.Info("bye")
			return nil
		case tui.Restart:
			log.Info("restarting to apply settings")
		case tui.LogOut:
			return a.logout()
		case tui.LogIn:
			if err := a.logout(); err != nil {
				return err
			}
		}
		// Go round again with the saved settings; it logs in if needed.
		if err := a.configure(); err != nil {
			return err
		}
	}
}

// app holds what one run of the UI needs.
type app struct {
	keeper       *awake.Keeper
	demo         bool
	configPath   string
	tokenPath    string
	speakerState string
	libraryCache string
	log          *slog.Logger

	// Set by configure from the config file.
	cfg        config.Config
	authn      *auth.Authenticator
	introShown bool
}

// configure (re)reads the config file, so settings changed in the UI take
// effect on restart.
func (a *app) configure() error {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	if _, err := art.ParseMode(cfg.Theme.CoverArt); err != nil {
		return fmt.Errorf("config %s: %w", a.configPath, err)
	}
	a.use(cfg)
	return nil
}

// use switches to cfg's settings, and the Web API app it names.
func (a *app) use(cfg config.Config) {
	webApp := auth.WebAPI
	if cfg.ClientID != "" {
		webApp = auth.WebAPIWith(cfg.ClientID, cfg.RedirectURI)
	}
	a.log.Info("configured", "cover art", cfg.Theme.CoverArt, "web api client", webApp.ClientID, "player", cfg.Player.Enabled)
	a.cfg = cfg
	a.authn = auth.New(webApp, a.tokenPath, a.log.With("pkg", "auth"))
}

// session logs in if needed, starts the speaker and runs the UI until the
// user leaves. It reports why they left.
func (a *app) session(ctx context.Context) (tui.Outcome, error) {
	opts := tui.Options{
		Config:     a.cfg,
		SaveConfig: func(c config.Config) error { return config.Save(a.configPath, c) },
		KeepAwake:  a.keeper.Set,
		Intro:      !a.introShown, // not again on restart
		Log:        a.log,
	}
	a.introShown = true
	var backend tui.Backend
	var spk *speaker.Speaker
	if a.demo {
		d := demo.New()
		d.Latency = 120 * time.Millisecond
		backend = d
		opts.LocalDevice = "Demo Laptop"
	} else {
		speakerToken, ok, err := a.login(ctx)
		if err != nil || !ok {
			return tui.Quit, err
		}
		opts.Config = a.cfg // the login screen may have set up the user's own app
		if a.cfg.Player.Enabled {
			if spk, err = a.startSpeaker(ctx, speakerToken); err != nil {
				return tui.Quit, err
			}
			defer spk.Close()
		}
		ts, err := a.authn.TokenSource(ctx, os.Stderr)
		if err != nil {
			return tui.Quit, err
		}
		client := spotify.New(auth.NewHTTPClient(ctx, ts), a.log.With("pkg", "spotify"))
		// Library reads go through a disk cache: fast, and far fewer calls
		// against Spotify's rate limit.
		library := cache.New(client, a.libraryCache, a.log.With("pkg", "cache"))
		var player tui.Player = client
		if spk != nil {
			// Play through the speaker directly: instant, and immune to Web
			// API rate limits. The Web API is only for browsing.
			player = spk
			opts.LocalDevice = a.cfg.Player.Name
			opts.PollInterval = time.Second
		}
		backend = struct {
			tui.Library
			tui.Player
			tui.LyricsSource
		}{library, player, lyrics.New(userAgent, a.log.With("pkg", "lyrics"))}
	}

	model := tui.New(backend, opts)
	program := tea.NewProgram(model, tea.WithContext(ctx))
	if spk != nil {
		go func() {
			<-spk.Done()
			if err := spk.Err(); err != nil {
				program.Send(tui.ErrMsg{Err: fmt.Errorf("sptui's speaker stopped: %w", err)})
			}
		}()
	}
	_, err := program.Run()
	a.keeper.Set(false) // the next session decides for itself
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return tui.Quit, err
	}
	return model.Outcome(), nil
}

// logout forgets the saved Spotify login and the speaker's copy of it.
func (a *app) logout() error {
	if a.demo {
		fmt.Println("Demo mode has no login to forget.")
		return nil
	}
	if err := errors.Join(a.authn.Logout(), speaker.Forget(a.speakerState), cache.Clear(a.libraryCache)); err != nil {
		return err
	}
	a.log.Info("logged out")
	fmt.Println("Logged out.")
	return nil
}

// login shows the login screen if any logins are missing — the speaker's
// and the Web API's — and reports whether the user went through with it.
// The approvals share one browser tab: Spotify shows its pages one after
// the other (or none, for apps the user already allowed). It returns a
// token for the speaker's first login, if it needed one.
func (a *app) login(ctx context.Context) (speakerToken string, ok bool, err error) {
	webNeeded := !a.authn.LoggedIn()
	speakerNeeded := a.cfg.Player.Enabled && !speaker.HasLogin(a.speakerState)
	if !webNeeded && !speakerNeeded {
		return "", true, nil
	}
	start := func(cfg config.Config) (tui.LoginAttempt, error) {
		a.use(cfg) // the user may have brought their own app
		var web, spk *auth.Pending
		cancel := func() {
			for _, p := range []*auth.Pending{web, spk} {
				if p != nil {
					p.Cancel()
				}
			}
		}
		var err error
		if !a.authn.LoggedIn() {
			if web, err = a.authn.Start(""); err != nil {
				return tui.LoginAttempt{}, err
			}
		}
		// A speaker approved on an earlier try needn't be approved again.
		if speakerNeeded && speakerToken == "" {
			next := "" // after approving, continue with the Web API login
			if web != nil {
				next = web.URL
			}
			if spk, err = auth.New(auth.Streaming, "", a.log.With("pkg", "auth")).Start(next); err != nil {
				cancel()
				return tui.LoginAttempt{}, err
			}
		}

		// The speaker's approval page comes first; it hands over to the Web API's.
		attempt := tui.LoginAttempt{Cancel: cancel}
		if spk != nil {
			attempt.Steps = append(attempt.Steps, tui.LoginStep{
				Purpose: auth.Streaming.Purpose,
				Wait: func(ctx context.Context) error {
					tok, err := spk.Wait(ctx)
					if err == nil {
						speakerToken = tok.AccessToken
					}
					return err
				},
			})
		}
		if web != nil {
			attempt.Steps = append(attempt.Steps, tui.LoginStep{
				Purpose: auth.WebAPI.Purpose,
				Wait: func(ctx context.Context) error {
					_, err := web.Wait(ctx)
					return err
				},
			})
		}
		switch {
		case spk != nil:
			attempt.URL = spk.URL
		case web != nil:
			attempt.URL = web.URL
		}
		return attempt, nil
	}
	ok, err = tui.RunLogin(ctx, tui.LoginOptions{
		Config:     a.cfg,
		SaveConfig: func(c config.Config) error { return config.Save(a.configPath, c) },
		Welcome:    webNeeded && (speakerNeeded || !a.cfg.Player.Enabled), // no login at all
		WebLogin:   webNeeded,
		Start:      start,
		OpenURL:    auth.OpenBrowser,
		Log:        a.log,
	})
	return speakerToken, ok, err
}

// startSpeaker runs sptui's built-in Spotify Connect device.
func (a *app) startSpeaker(ctx context.Context, token string) (*speaker.Speaker, error) {
	creds := speaker.Credentials{AccessToken: token}
	return speaker.Start(ctx, speaker.Config{
		Name:      a.cfg.Player.Name,
		Backend:   a.cfg.Player.Backend,
		Bitrate:   a.cfg.Player.Bitrate,
		StatePath: a.speakerState,
	}, creds, a.log.With("pkg", "speaker"))
}
