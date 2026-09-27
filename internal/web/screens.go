package web

import (
	"fmt"
	"image/color"
	"net/http"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/demo"
	"github.com/kyledickey/sptui/internal/logging"
	"github.com/kyledickey/sptui/internal/tui"
)

// The size the demo is drawn at, in cells.
const screenW, screenH = 112, 34

// demoScreens drives sptui -demo to the screens the website shows, as text
// with ANSI styles, by name: home with a song playing, and now playing.
func demoScreens() map[string]string {
	cfg := config.Default()
	cfg.Theme.CoverArt = art.Blocks.String()
	app := tui.NewHeadless(demo.New(), tui.Options{Config: cfg, Log: logging.Discard()}, screenW, screenH)
	app.Open("Liked Songs")
	app.Press("down", "down", "enter") // play the third song
	app.Open("Home")
	screens := map[string]string{"home": app.View()}
	app.Press("o", "2") // hide the lyrics: the demo's are random words
	screens["playing"] = app.View()
	return screens
}

// screenBG is the background of cells that don't set one: the terminal's,
// which the intros assume too.
const screenBG = "#101216"

// screen is a screen as cells for the website to draw. Colors are
// indexes into Colors, plus one, so 0 is the default.
type screen struct {
	W      int      `json:"w"`
	H      int      `json:"h"`
	BG     string   `json:"bg"`
	Colors []string `json:"colors"`
	Cells  [][6]any `json:"cells"` // x, y, char, fg, bg, bold, for cells with something in them
}

// parseScreen reads a screen of text with ANSI styles w cells wide.
func parseScreen(ans string) screen {
	lines := strings.Split(strings.TrimRight(ans, "\n"), "\n")
	w := 0
	for _, l := range lines {
		w = max(w, uv.NewStyledString(l).Bounds().Dx())
	}
	buf := uv.NewScreenBuffer(w, len(lines))
	uv.NewStyledString(strings.Join(lines, "\n")).Draw(buf, buf.Bounds())

	s := screen{W: w, H: len(lines), BG: screenBG, Cells: [][6]any{}}
	index := map[string]int{}
	colorOf := func(c color.Color) int {
		if c == nil {
			return 0
		}
		r, g, b, _ := c.RGBA()
		hex := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
		i, ok := index[hex]
		if !ok {
			s.Colors = append(s.Colors, hex)
			i = len(s.Colors)
			index[hex] = i
		}
		return i
	}
	for y := range s.H {
		for x := range s.W {
			c := buf.CellAt(x, y)
			if c == nil || (strings.TrimSpace(c.Content) == "" && c.Style.Bg == nil) {
				continue
			}
			s.Cells = append(s.Cells, [6]any{x, y, c.Content, colorOf(c.Style.Fg), colorOf(c.Style.Bg), c.Style.Attrs&uv.AttrBold != 0})
		}
	}
	return s
}

// screens serves /screens/{name}.json. They're drawn once, by the same
// code as the app, so they always match it, and kept gzipped.
type screens struct {
	once sync.Once
	json map[string][]byte
}

func (s *screens) load() {
	s.once.Do(func() {
		s.json = map[string][]byte{}
		for name, ans := range demoScreens() {
			s.json[name] = gzipJSON(parseScreen(ans))
		}
	})
}

func (s *screens) serve(w http.ResponseWriter, r *http.Request) {
	s.load()
	name, ok := strings.CutSuffix(r.PathValue("file"), ".json")
	data, found := s.json[name]
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	writeGzipJSON(w, r, data)
}
