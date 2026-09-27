package tui

import (
	"context"
	"fmt"
	"image"
	"math"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/kyledickey/sptui/internal/spotify"
)

// Colors taken from cover art: the accent that follows the playing song
// (theme.accent = "cover"), and the swatches of a playlist's color strip.
// Both come from small copies of the covers, fetched in the background.

const (
	// AccentFromCover is the accent setting that follows the cover art.
	AccentFromCover = "cover"

	swatchSource   = 64  // source image width for colors, in pixels
	swatchInFlight = 6   // covers fetched at once for colors
	swatchMax      = 400 // colors remembered
)

// swatch is what a cover looks like in two colors, as hex.
type swatch struct {
	average string // the whole cover, averaged
	vivid   string // its most striking hue, bright enough to read; "" if grey
}

type swatchMsg struct {
	url string
	sw  swatch
	err error
}

// swatches holds colors by image URL.
type swatches struct {
	known   map[string]swatch
	pending map[string]bool // being fetched, or failed
	fetches int             // in flight
}

func newSwatches() swatches {
	return swatches{known: map[string]swatch{}, pending: map[string]bool{}}
}

// swatchURL is the image to take a track's colors from.
func swatchURL(t spotify.Track) string {
	return spotify.CoverURL(t.Cover(), swatchSource)
}

// playlistSwatchURL is the image to take a playlist's color from.
func playlistSwatchURL(pl spotify.Playlist) string {
	return spotify.CoverURL(pl.Images, swatchSource)
}

// swatchColor is the color a cover shows as on its own: its vivid hue,
// else its average. ok is false until the colors arrive.
func (m *Model) swatchColor(url string) (hex string, ok bool) {
	sw, ok := m.swatches.known[url]
	switch {
	case !ok:
		return "", false
	case sw.vivid != "":
		return sw.vivid, true
	case sw.average != "":
		return stripColor(sw.average), true
	}
	return "", false
}

// wantedSwatches lists the images whose colors are on screen: the playing
// song's when the accent follows it, the sidebar's playlists, and a
// playlist strip's.
func (m *Model) wantedSwatches() []string {
	var want []string
	if t := m.player.track(); t != nil && m.cfg.Theme.Accent == AccentFromCover {
		if url := swatchURL(*t); url != "" {
			want = append(want, url)
		}
	}
	if !m.showingNowPlaying() {
		sb := &m.sidebar
		top := min(sb.scroll, len(sb.items))
		for _, it := range sb.items[top:min(top+m.bodyHeight(), len(sb.items))] {
			if it.playlist == nil {
				continue
			}
			if url := playlistSwatchURL(*it.playlist); url != "" && !slices.Contains(want, url) {
				want = append(want, url)
			}
		}
	}
	if p := m.current(); p != nil && p.strip {
		for _, r := range p.rows {
			if len(want) >= swatchMax/2 {
				break // plenty for a strip, and the cache never has to churn
			}
			if url := swatchURL(r.track); r.kind == kindTrack && url != "" && !slices.Contains(want, url) {
				want = append(want, url)
			}
		}
	}
	return want
}

// syncSwatches fetches colors that are wanted but missing, a few at a time.
func (m *Model) syncSwatches() tea.Cmd {
	s := &m.swatches
	var cmds []tea.Cmd
	for _, url := range m.wantedSwatches() {
		if s.fetches >= swatchInFlight {
			break
		}
		if _, ok := s.known[url]; ok || s.pending[url] {
			continue
		}
		s.pending[url] = true
		s.fetches++
		cmds = append(cmds, m.call(func(ctx context.Context) tea.Msg {
			img, err := m.backend.CoverArt(ctx, url)
			if err != nil {
				return swatchMsg{url: url, err: err}
			}
			return swatchMsg{url: url, sw: swatchOf(img)}
		}))
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleSwatch(msg swatchMsg) {
	s := &m.swatches
	s.fetches--
	if msg.err != nil {
		m.log.Debug("cover colors failed", "url", msg.url, "err", msg.err)
		return // stays pending: not retried this session
	}
	if len(s.known) >= swatchMax {
		clear(s.known) // cheap to fetch again
	}
	s.known[msg.url] = msg.sw
	delete(s.pending, msg.url)
	m.syncAccent()
}

// accentHex is the accent to draw with right now.
func (m *Model) accentHex() string {
	if m.cfg.Theme.Accent != AccentFromCover {
		return m.cfg.Theme.Accent
	}
	if t := m.player.track(); t != nil {
		if sw := m.swatches.known[swatchURL(*t)]; sw.vivid != "" {
			return sw.vivid
		}
	}
	return DefaultAccent
}

// syncAccent restyles the UI when the accent should change, e.g. when a
// new song's cover colors arrive.
func (m *Model) syncAccent() {
	if hex := m.accentHex(); hex != m.accentShown {
		m.setTheme(m.dark)
	}
}

// swatchOf finds a cover's colors. It samples a grid of pixels; the vivid
// color is the average of the most common saturated hue, weighted by how
// saturated and bright each pixel is.
func swatchOf(img image.Image) swatch {
	b := img.Bounds()
	step := max(1, max(b.Dx(), b.Dy())/48)
	const bins = 12
	var sum [3]float64
	var hue [bins][4]float64 // r, g, b, weight
	n := 0.0
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r16, g16, b16, _ := img.At(x, y).RGBA()
			r, g, bl := float64(r16>>8)/255, float64(g16>>8)/255, float64(b16>>8)/255
			sum[0], sum[1], sum[2] = sum[0]+r, sum[1]+g, sum[2]+bl
			n++
			h, s, v := hsv(r, g, bl)
			if s < 0.25 || v < 0.2 {
				continue
			}
			w := s * v
			i := int(h*bins) % bins
			hue[i][0] += r * w
			hue[i][1] += g * w
			hue[i][2] += bl * w
			hue[i][3] += w
		}
	}
	if n == 0 {
		return swatch{}
	}
	sw := swatch{average: hexOf(sum[0]/n, sum[1]/n, sum[2]/n)}
	best := 0
	for i := range hue {
		if hue[i][3] > hue[best][3] {
			best = i
		}
	}
	if w := hue[best][3]; w > n*0.02 {
		h, s, _ := hsl(hue[best][0]/w, hue[best][1]/w, hue[best][2]/w)
		sw.vivid = hexOf(fromHSL(h, max(s, 0.55), 0.62))
	}
	return sw
}

// readable shifts a color's lightness so it reads on the background.
func readable(hex string, dark bool) string {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return hex
	}
	h, s, l := hsl(r, g, b)
	if dark {
		l = min(max(l, 0.55), 0.75)
	} else {
		l = min(max(l, 0.3), 0.45)
	}
	return hexOf(fromHSL(h, s, l))
}

// stripColor is a cover's average, kept away from black and white so every
// song shows up in the strip.
func stripColor(hex string) string {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return hex
	}
	h, s, l := hsl(r, g, b)
	return hexOf(fromHSL(h, min(s*1.3, 1), min(max(l, 0.3), 0.7)))
}

// parseHex reads a color like "#1ed760" as red, green and blue in 0–1.
func parseHex(hex string) (r, g, b float64, ok bool) {
	var ri, gi, bi int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &ri, &gi, &bi); err != nil {
		return 0, 0, 0, false
	}
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255, true
}

// hexOf writes red, green and blue in 0–1 as a color like "#1ed760".
func hexOf(r, g, b float64) string {
	c := func(v float64) int { return int(math.Round(min(max(v, 0), 1) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", c(r), c(g), c(b))
}

func hsv(r, g, b float64) (h, s, v float64) {
	hi, lo := max(r, g, b), min(r, g, b)
	v = hi
	if hi > 0 {
		s = (hi - lo) / hi
	}
	return hueOf(r, g, b, hi, lo), s, v
}

func hsl(r, g, b float64) (h, s, l float64) {
	hi, lo := max(r, g, b), min(r, g, b)
	l = (hi + lo) / 2
	if d := hi - lo; d > 0 {
		s = d / (1 - math.Abs(2*l-1))
	}
	return hueOf(r, g, b, hi, lo), min(s, 1), l
}

// hueOf is the hue in [0, 1).
func hueOf(r, g, b, hi, lo float64) float64 {
	d := hi - lo
	if d == 0 {
		return 0
	}
	var h float64
	switch hi {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h /= 6
	if h < 0 {
		h++
	}
	return h
}

func fromHSL(h, s, l float64) (r, g, b float64) {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h*6, 2)-1))
	m := l - c/2
	switch int(h*6) % 6 {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}
