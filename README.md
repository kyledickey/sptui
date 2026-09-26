# sptui

Spotify in your terminal. A clean, keyboard-first (mouse works too) Spotify
client written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

sptui plays music itself — no other Spotify app needed. It shows up in
Spotify's device list as **sptui**, so you can also control it from your phone,
or send its playback to another speaker with `d`. Media keys work on Linux
(MPRIS). Spotify Premium is required.

## Install

sptui decodes audio with a few C libraries, so it needs cgo and:

| System          | Command                                                                              |
| --------------- | ------------------------------------------------------------------------------------ |
| Arch / CachyOS  | `sudo pacman -S libogg libvorbis flac mpg123 alsa-lib`                               |
| Debian / Ubuntu | `sudo apt install libogg-dev libvorbis-dev libflac-dev libmpg123-dev libasound2-dev` |
| macOS           | `brew install libogg libvorbis flac mpg123`                                          |

Then:

```sh
go install ./cmd/sptui
sptui
```

The first run opens your browser to log in to Spotify. You'll approve two
apps back to back in the same tab, one to play music and one to browse your
library (Spotify rate-limits each app separately, so sptui uses one for each,
like spotify-player). That's the only setup: no developer account or Client
ID.

The login is saved in `~/.cache/sptui/token.json` (and the built-in speaker's
copy in `speaker.json`). To log out or switch accounts, press `u` in sptui, or
run `sptui logout`.

Want to look around first? `sptui -demo` uses made-up music and needs no
account.

### Settings

Press `,` for the settings screen: colours, album art on/off, the now-playing
cover size, keeping the screen awake, the built-in player (on/off, device
name, audio output, quality),
your own Spotify app, and logging out. Changes are saved as you make them.
Appearance changes apply at once; the rest offer a one-key restart.

### Config

Optional — everything here can also be changed on the settings screen. `~/.config/sptui/config.toml` is created on first run with every
setting commented out:

```toml
client_id = ""          # your own Spotify app, see "Rate limits" below
redirect_uri = "http://127.0.0.1:8989/login"

[player]
enabled = true          # false: only control other devices
name = "sptui"          # name in Spotify's device list
backend = ""            # "pulseaudio" (also PipeWire), "alsa", ... ; "" = auto
bitrate = 320           # 96, 160 or 320

[theme]
accent = "#1ed760"
cover_art = "auto"      # "kitty", "blocks", "off"
```

### Rate limits

Spotify limits how often each app may call its Web API, and the app sptui
browses with is shared with other terminal clients, so it's often busy.
sptui keeps its use low:

- Playback never touches the Web API: sptui drives its built-in speaker
  directly, so music keeps working even when Spotify is refusing requests.
- Your library is cached on disk (`~/.cache/sptui/library`). Startup and
  pages you've visited recently need no requests at all, and when Spotify
  is refusing requests you see the saved copy instead of an error.
  Cached pages show a dim `◷ 12m ago` by their title (amber `· Spotify
busy` when it's a fallback), and `ctrl+r` reloads from Spotify.
- When Spotify says to wait, sptui waits instead of retrying.

For a limit that's yours alone, make your own Spotify app (about two
minutes):

1. Open <https://developer.spotify.com/dashboard> and click **Create app**.
2. Name and description can be anything. Under **Redirect URIs** add
   `http://127.0.0.1:8989/login`, tick **Web API**, agree, and save.
3. Open the app's **Settings** and copy its **Client ID**.
4. In sptui press `,`, select **Your own Spotify app**, paste the ID, and
   choose **Restart sptui to apply changes**. You'll approve the new app once.

### Keeping the screen awake

While music plays, sptui asks your desktop not to dim or turn off the screen
and not to go to sleep when idle, like video and music players do (closing
the lid or choosing Sleep still works). Change it under **Keep screen awake**
in settings: while playing (default), always while sptui is open, or off.
Works on Linux desktops (KDE, GNOME and others, over D-Bus) and macOS.

### Now playing and lyrics

Press `o` (or click the player bar) for a full-screen view with three
panels: **1** the track (cover, song, controls), **2** synced lyrics that
follow along, **3** what's up next. Like btop, press a panel's number to hide
or show it — `2` alone makes a lyrics screen, `1` alone a big album-art
screen — and sptui remembers your choice. `o` or `esc` goes back. Lyrics come from [LRCLIB](https://lrclib.net), a free
community lyrics database, so they don't count against Spotify's rate limit.

### Podcasts

Podcasts play like everything else. **Podcasts** in the sidebar lists the
shows you follow, and **Your Episodes** the episodes you've saved; search
(`/`) finds both too. Open a podcast to see its episodes, newest first. `l`
follows a podcast or saves an episode.

### Album art

Covers show in the player bar and at the top of album, playlist and artist
pages. In terminals that support kitty's graphics protocol with Unicode
placeholders (kitty, Ghostty) they're real images; everywhere else they're
drawn with coloured half-block "pixels". `auto` picks for you.

Inside tmux, auto uses blocks. For real images there, add
`set -g allow-passthrough on` to `tmux.conf` and set `cover_art = "kitty"`.

### Commands and flags

|                                |                                               |
| ------------------------------ | --------------------------------------------- |
| `sptui`                        | start                                         |
| `sptui login` / `sptui logout` | switch account / forget the login             |
| `-demo`                        | fake library, no account needed               |
| `-debug`                       | include debug messages in the log             |
| `-log path`                    | log file (default `~/.cache/sptui/sptui.log`) |
| `-config path`                 | config file                                   |

## Using it

Two panes: the **sidebar** (your library and playlists) and the **list**.
`tab` switches between them, `enter` plays or opens, `esc` goes back.
The footer always shows the keys that matter right now, and `?` shows all of
them. When in doubt, press **`m`** on anything to see what you can do with it.

| Key         |                               | Key       |                         |
| ----------- | ----------------------------- | --------- | ----------------------- |
| `↑↓` / `jk` | move                          | `space`   | play / pause            |
| `enter`     | play / open                   | `n` / `p` | next / previous         |
| `esc`       | back                          | `[` / `]` | seek ∓10s               |
| `tab`, `←→` | switch pane                   | `+` / `-` | volume                  |
| `/`         | search (your playlists first) | `s` / `r` | shuffle / repeat        |
| `f`         | filter this list              | `d`       | pick a device           |
| `m`         | actions for selection         | `M`       | actions for now playing |
| `l`         | like / save / follow          | `L`       | like now playing        |
| `a`         | add to queue                  | `ctrl+r`  | reload                  |
| `u`         | account / log out             | `o`       | now playing + lyrics    |
| `,`         | settings                      |           |                         |
| `g` / `G`   | top / bottom                  | `q`       | quit                    |

Mouse: scroll with the wheel, click to select, click again to play or open.

### Things Spotify doesn't allow

Spotify's Web API (as of February 2026) limits some features, so sptui can't
offer them: listing the songs of playlists you don't own or collaborate on
(you can still play them), artist top tracks, and browse/new releases.

## Code layout

Each package is a small piece with one job; `cmd/sptui` plugs them together.

```
cmd/sptui          wiring: flags, config, login, then start the UI
internal/config    config file + env vars
internal/logging   slog to a file (the UI owns the terminal)
internal/auth      browser login (OAuth PKCE), token saved and refreshed on disk
internal/spotify   thin Web API client and types; takes any *http.Client
internal/speaker   the built-in Spotify Connect speaker (wraps go-librespot)
internal/tui       the Bubble Tea UI; talks to Spotify only via tui.Backend
internal/art       draws cover images: kitty graphics or half-block pixels
internal/lyrics    finds (synced) lyrics on LRCLIB
internal/cache     disk cache in front of the Web API library calls
internal/awake     keeps the computer and screen awake (D-Bus, caffeinate)
internal/demo      in-memory fake Spotify implementing tui.Backend
```

`tui.Backend` (in `internal/tui/backend.go`) is the seam between the UI and
Spotify. `*spotify.Client` implements it for real; `*demo.Backend` implements it
for `-demo` and tests.

Playback: `internal/speaker` runs
[go-librespot](https://github.com/devgianlu/go-librespot) in-process as a
Spotify Connect device, and also implements `tui.Player` by talking to it
directly, so playback never uses the Web API. `cmd/sptui` pairs it with
`*spotify.Client` as the `tui.Library`. The speaker logs in through Spotify's
desktop client (`auth.Streaming`); the Web API uses a separate app
(`auth.WebAPI`) because Spotify rate-limits each app as a whole.

go-librespot is GPL-3.0, so builds of sptui that include it are too.

```sh
go test ./...        # includes UI tests driven against the demo backend
```
