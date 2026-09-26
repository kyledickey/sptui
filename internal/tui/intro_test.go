package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kyledickey/sptui/internal/art"
	"github.com/kyledickey/sptui/internal/demo"
	"github.com/kyledickey/sptui/internal/logging"
)

func TestIntro(t *testing.T) {
	newIntro := func() *Model {
		m := New(demo.New(), Options{Config: withArt(art.Off), Intro: true, Log: logging.Discard()})
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		return m
	}

	m := newIntro()
	m.me.DisplayName = "Ada Lovelace"
	m.intro.start = time.Now().Add(-time.Duration(m.intro.Anim.Fade) * time.Millisecond)
	if screen := ansi.Strip(m.View().Content); !strings.Contains(screen, "hi, ada") {
		t.Errorf("intro doesn't greet by name:\n%s", screen)
	}

	m.intro.start = time.Now().Add(-time.Duration(m.intro.Anim.End) * time.Millisecond)
	m.Update(introTickMsg{})
	if m.intro != nil {
		t.Error("intro still playing after it ended")
	}

	m = newIntro()
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.intro != nil {
		t.Error("a key didn't skip the intro")
	}

	cfg := withArt(art.Off)
	cfg.Theme.Intro = "off"
	m = New(demo.New(), Options{Config: cfg, Intro: true, Log: logging.Discard()})
	if m.intro != nil {
		t.Error("intro plays when it's off")
	}
}
