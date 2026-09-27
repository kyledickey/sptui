package demo

import (
	"context"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/kyledickey/sptui/internal/lyrics"
	"github.com/kyledickey/sptui/internal/spotify"
)

var lyricWords = []string{"we", "run", "through", "the", "neon", "light", "hold", "on", "tonight", "your",
	"heart", "is", "an", "echo", "in", "my", "hands", "falling", "slow", "like", "summer", "rain", "never", "let", "go"}

// Lyrics makes up synced lyrics, one line every few seconds. Every third
// song is an instrumental, and every seventh has none, to show those states.
func (b *Backend) Lyrics(ctx context.Context, t spotify.Track) (lyrics.Lyrics, error) {
	if err := b.wait(ctx); err != nil {
		return lyrics.Lyrics{}, err
	}
	b.mu.Unlock()
	h := fnv.New64a()
	h.Write([]byte(t.URI))
	seed := h.Sum64()
	switch {
	case seed%7 == 0:
		return lyrics.Lyrics{}, lyrics.ErrNotFound
	case seed%3 == 0:
		return lyrics.Lyrics{Instrumental: true}, nil
	}
	r := rand.New(rand.NewPCG(seed, 1))
	var l lyrics.Lyrics
	l.Synced = true
	for at := 8 * time.Second; at < t.Duration()-5*time.Second; at += time.Duration(3+r.IntN(3)) * time.Second {
		words := make([]string, 3+r.IntN(5))
		for i := range words {
			words[i] = lyricWords[r.IntN(len(lyricWords))]
		}
		text := strings.Join(words, " ")
		if r.IntN(9) == 0 {
			text = "" // an instrumental break
		}
		l.Lines = append(l.Lines, lyrics.Line{At: at, Text: text})
	}
	return l, nil
}
