package web

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/kyledickey/sptui/internal/config"
	"github.com/kyledickey/sptui/internal/intro"
	"github.com/kyledickey/sptui/internal/tui"
)

// introFrame is one frame of an animation for the website to draw. Colors
// are indexes into introAnim.Colors, plus one, so 0 is none.
type introFrame struct {
	Fade   float64  `json:"fade"`
	Pixels []int    `json:"px"`    // 2H rows of W
	Cells  [][4]any `json:"cells"` // x, y, char, color, for cells with one
}

type introAnim struct {
	Name   string       `json:"name"`
	W      int          `json:"w"`
	H      int          `json:"h"`
	Frame  int          `json:"frame"` // ms between frames
	BG     string       `json:"bg"`
	Colors []string     `json:"colors"`
	Frames []introFrame `json:"frames"`
}

// encodeIntro draws every frame of a with the app's default dark theme.
func encodeIntro(a intro.Anim) introAnim {
	env := tui.IntroEnv(config.Config{}, true, "", "")
	p := intro.NewPlay(a, env)
	step := intro.Frame.Milliseconds()
	out := introAnim{Name: a.Name, Frame: int(step), BG: env.BG}
	index := map[string]int{}
	color := func(hex string) int {
		if hex == "" {
			return 0
		}
		i, ok := index[hex]
		if !ok {
			out.Colors = append(out.Colors, hex)
			i = len(out.Colors)
			index[hex] = i
		}
		return i
	}
	for t := 0.0; t <= a.End; t += float64(step) {
		g := p.Grid(t)
		out.W, out.H = g.W, g.H
		f := introFrame{Fade: g.Fade, Pixels: make([]int, 0, 2*g.H*g.W), Cells: [][4]any{}}
		for _, row := range g.Pixels {
			for _, hex := range row {
				f.Pixels = append(f.Pixels, color(hex))
			}
		}
		for y, row := range g.Cells {
			for x, c := range row {
				if c.Char != "" {
					f.Cells = append(f.Cells, [4]any{x, y, c.Char, color(c.Color)})
				}
			}
		}
		out.Frames = append(out.Frames, f)
	}
	return out
}

// intros serves the startup animations as JSON: /intros.json lists their
// names, and /intros/{name}.json is every frame of one. They're drawn once,
// on first request, and kept gzipped.
type intros struct {
	once  sync.Once
	index []byte
	anims map[string][]byte
}

func (s *intros) load() {
	s.once.Do(func() {
		s.index = gzipJSON(intro.Names())
		s.anims = map[string][]byte{}
		for _, a := range intro.All {
			s.anims[a.Name] = gzipJSON(encodeIntro(a))
		}
	})
}

func (s *intros) serveIndex(w http.ResponseWriter, r *http.Request) {
	s.load()
	writeGzipJSON(w, r, s.index)
}

func (s *intros) serveAnim(w http.ResponseWriter, r *http.Request) {
	s.load()
	name, ok := strings.CutSuffix(r.PathValue("file"), ".json")
	data, found := s.anims[name]
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	writeGzipJSON(w, r, data)
}

func gzipJSON(v any) []byte {
	var b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		panic(err) // only our own types, which always encode
	}
	zw.Close()
	return b.Bytes()
}

// writeGzipJSON sends gzipped JSON, unzipping it for the rare client that
// can't take gzip.
func writeGzipJSON(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Vary", "Accept-Encoding")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(data)
		return
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var b bytes.Buffer
	b.ReadFrom(zr)
	w.Write(b.Bytes())
}
