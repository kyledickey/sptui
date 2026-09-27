package intro

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var testEnv = Env{
	Accent: "#1ed760", Purple: "#b18cff", Text: "#e6e6e6", Muted: "#9a9a9a", Faint: "#5a5a5a",
	BG: "#101216", Dark: true, Name: "Ada Lovelace",
}

// Every animation draws every frame without trouble.
func TestScenes(t *testing.T) {
	for _, a := range All {
		p := NewPlay(a, testEnv)
		for ms := 0.0; ms <= a.End; ms += 16 {
			c := a.scene(p, ms)
			if c.w > 60 || c.h > 16 {
				t.Fatalf("%s: scene %d×%d is bigger than the smallest screen", a.Name, c.w, c.h)
			}
		}
	}
}

func TestGreeting(t *testing.T) {
	for _, a := range All {
		p := NewPlay(a, testEnv)
		if frame := ansi.Strip(p.Frame(a.Fade)); !strings.Contains(frame, "ada") {
			t.Errorf("%s doesn't greet by name:\n%s", a.Name, frame)
		}
	}
}
