# [sptui](https://sptui.sh)

Spotify in your terminal. A clean, keyboard-first (mouse works too) Spotify
client written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

sptui is a Spotify player itself, you don't need any other app. It shows up in
Spotify's device list as **sptui**, so you can also control it from your phone,
or send its playback to another speaker with `d`. **Spotify Premium is
required**.

## Install

```sh
curl -fsSL https://sptui.sh/install.sh | bash
```

That downloads sptui from the [latest release](https://github.com/kyledickey/sptui/releases/latest)
into `~/.local/bin`, for macOS or Linux (x86-64 or ARM). The audio codecs are
built in, so there's nothing else to install; on Linux it also installs ALSA's
library if it's missing.

### Updating

sptui checks for a new release once a day. When there is one, a note at the
top of the sidebar says so: press `U` to install it while the music keeps
playing, then `U` again to restart. Or, from a terminal:

```sh
sptui update
```

### By hand

sptui decodes audio with a few C libraries, so it needs cgo and:

| System          | Command                                                                              |
| --------------- | ------------------------------------------------------------------------------------ |
| Arch / CachyOS  | `sudo pacman -S libogg libvorbis flac mpg123 alsa-lib`                               |
| Debian / Ubuntu | `sudo apt install libogg-dev libvorbis-dev libflac-dev libmpg123-dev libasound2-dev` |
| macOS           | `brew install libogg libvorbis flac mpg123`                                          |

_these are the only OSes I've tested on, others probably work_

Then:

```sh
go install github.com/kyledickey/sptui/cmd/sptui@latest
sptui
```

The first run opens your browser to log in to Spotify. You'll approve two
apps, one to play music and one to browse your library. To log out or switch accounts, press `u` in sptui, or run `sptui logout`.

Want to look around first? `sptui -demo` uses made-up music and needs no
account.

### Settings

Press `,` to open settings. Changes are saved as you make them.
Appearance changes apply at once; the rest need a restart.

#### (optional) Config

Everything in the settings can also be set in the config. `~/.config/sptui/config.toml` is created on first run with every setting commented out.

### Rate limits

Spotify is extremely restrictive with their API. Because of that sptui comes with a default client and it gets rate limited very quickly. It tries to mitigate this by running its own player and caching your library. But, it still is restrictive.

**To get rid of the rate limits,** you can make a Spotify app, it takes 2 minutes:

1. Open [developer.spotify.com](https://developer.spotify.com/dashboard) and click **Create app**.
2. The name and description can be anything. Under **Redirect URIs** add
   `http://127.0.0.1:8989/login`, tick **Web API**, agree, and save.
3. Copy its **Client ID** on the next page or in the settings.
4. sptui will ask for your client ID, if it doesn't open settings with `,` and
   paste it in there.

### Now playing and lyrics

Press `o` (or click the player bar) for a full-screen view with three
panels: **1** the track (cover, song, controls), **2** lyrics that
follow along, **3** the queue. Press a panel's number to hide
or show it. `o` or `esc` goes back. Lyrics come from [LRCLIB](https://lrclib.net).

### Podcasts

Podcasts play like everything else. **Podcasts** in the sidebar lists the
shows you follow. Open a podcast to see its episodes, newest first. `l`
follows a podcast or saves an episode.

### Album art

In terminals that support kitty's graphics protocol with Unicode
placeholders (kitty, Ghostty) real images are rendered, otherwise they are
pixelated.

### Commands and flags

|                                |                                               |
| ------------------------------ | --------------------------------------------- |
| `sptui`                        | start                                         |
| `sptui login` / `sptui logout` | switch account / forget the login             |
| `sptui update`                 | install the latest release (`-check`: look)   |
| `-demo`                        | fake library, no account needed               |
| `-debug`                       | include debug messages in the log             |
| `-log path`                    | log file (default `~/.cache/sptui/sptui.log`) |
| `-config path`                 | config file                                   |

## Using it

Switch between the two main panes with`tab`. Hitting `enter` plays or opens,
`esc` goes back. The footer shows key hints, and `?` shows all of them. When in
doubt, press **`m`** on anything to see what you can do with it.

| Key         |                               | Key       |                          |
| ----------- | ----------------------------- | --------- | ------------------------ |
| `↑↓` / `jk` | move                          | `space`   | play / pause             |
| `enter`     | play / open                   | `n` / `p` | next / previous          |
| `esc`       | back                          | `[` / `]` | seek ∓10s                |
| `tab`, `←→` | switch pane                   | `+` / `-` | volume                   |
| `/`         | search (your playlists first) | `s` / `r` | shuffle / repeat         |
| `f`         | filter this list              | `d`       | pick a device            |
| `m`         | actions for selection         | `M`       | actions for now playing  |
| `l`         | like / save / follow          | `L`       | like now playing         |
| `a`         | add to queue                  | `ctrl+r`  | reload                   |
| `u`         | account / log out             | `o`       | now playing + lyrics     |
| `,`         | settings                      | `P` / `S` | play / shuffle this page |
| `1`–`6`     | search result tabs            | `.`       | actions for this page    |
| `g` / `G`   | top / bottom                  | `U`       | update sptui             |
|             |                               | `q`       | quit                     |

Mouse: scroll with the wheel, click to select, click again to play or open.

### Things Spotify doesn't allow

Spotify's Web API (as of February 2026) limits some features, so sptui can't
offer them: listing the songs of playlists you don't own or collaborate on
(you can still play them), artist top tracks, and browse/new releases.

### How playback works

`internal/speaker` runs
[go-librespot](https://github.com/devgianlu/go-librespot) as a
Spotify Connect device. The speaker logs in through Spotify's
desktop client (`auth.Streaming`); the Web API uses a separate app
(`auth.WebAPI`) because Spotify rate-limits each app as a whole.

go-librespot is GPL-3.0, so builds of sptui that include it are too.

## Developing

You'll need Go (see `go.mod` for the version) and the audio libraries from
[By hand](#by-hand), plus `pkg-config`.

```sh
go run ./cmd/sptui -demo        # run against the fake library
go test ./...                   # tests
go vet ./... && gofmt -l .      # what CI checks
```

Also in `cmd/`:

- `sptui-intros` plays the startup animations on a loop, with pause, frame
  stepping, scrubbing and slow motion.
- `web` serves the website (`internal/web/site`); it doesn't need cgo, so
  `CGO_ENABLED=0 go run ./cmd/web` works without the audio libraries.

Logs go to `~/.cache/sptui/sptui.log`; add `-debug` for more.

## Contributing

Issues and pull requests are welcome. For anything bigger than a small fix,
open an issue first so we can talk it over. Before sending a PR, make sure
`gofmt`, `go vet` and `go test ./...` pass, and keep changes focused.

## License

[GPL-3.0](LICENSE).
