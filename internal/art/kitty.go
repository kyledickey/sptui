package art

import (
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// Kitty image IDs are carried in a 256-color foreground, which survives
// every color profile unchanged. IDs below 16 are avoided because those are
// the basic colors, which renderers may rewrite as 30–37.
const (
	MinID = 16
	MaxID = 255
)

// KittyTransmit returns the escape sequence that uploads img under id and
// creates a virtual placement cols×rows cells big. The terminal scales the
// image to fit, keeping its aspect ratio.
func KittyTransmit(id int, img image.Image, cols, rows int) (string, error) {
	var b strings.Builder
	err := kitty.EncodeGraphics(&b, img, &kitty.Options{
		Action:           kitty.TransmitAndPut,
		Transmission:     kitty.Direct,
		Format:           kitty.PNG,
		ID:               id,
		Columns:          cols,
		Rows:             rows,
		VirtualPlacement: true,
		Quiet:            2, // no replies; they'd arrive as stray input
		Chunk:            true,
		ChunkFormatter:   passthrough,
	})
	if err != nil {
		return "", fmt.Errorf("encode image: %w", err)
	}
	return b.String(), nil
}

// KittyDelete returns the sequence that frees image id in the terminal.
func KittyDelete(id int) string {
	opts := &kitty.Options{Action: kitty.Delete, Delete: kitty.DeleteID, DeleteResources: true, ID: id, Quiet: 2}
	return passthrough(ansi.KittyGraphics(nil, opts.Options()...))
}

// KittyPlaceholder returns the cells that show image id: a grid of
// placeholder characters whose diacritics give each cell's row and column.
func KittyPlaceholder(id, cols, rows int) string {
	var b strings.Builder
	for r := range rows {
		if r > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
		for c := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[39m")
	}
	return b.String()
}

// passthrough wraps seq so tmux forwards it to the real terminal. That needs
// "set -g allow-passthrough on" in tmux.conf.
func passthrough(seq string) string {
	if os.Getenv("TMUX") != "" {
		return ansi.TmuxPassthrough(seq)
	}
	return seq
}
