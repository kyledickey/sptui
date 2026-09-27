package banner

import "testing"

func TestBanner(t *testing.T) {
	big, ok := Spell("Hi 5")
	if !ok || big[0] != "█▄█ ▀█▀    ██▀" || big[1] != "█ █ ▄█▄    ▄▄▀" {
		t.Fatalf("Spell = %q, %v", big, ok)
	}
	if _, ok := Spell("Björk"); ok {
		t.Fatal("letters outside the font should fall back")
	}
}
