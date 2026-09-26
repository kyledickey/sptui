// Package awake stops the computer from going to sleep, and the screen from
// dimming or turning off, while sptui asks it to — the way music and video
// players do. It only holds off idle sleep: closing the lid or choosing
// Sleep yourself still works.
package awake

import "log/slog"

// Keeper holds the computer awake on request. Its methods don't block;
// talking to the system happens in the background.
type Keeper struct {
	app  string
	log  *slog.Logger
	want chan bool
	done chan struct{}
}

// New starts a Keeper. app names the program to the system, which may show
// it (e.g. KDE's battery applet lists who's keeping the screen on).
func New(app string, log *slog.Logger) *Keeper {
	k := &Keeper{app: app, log: log, want: make(chan bool, 1), done: make(chan struct{})}
	go k.run()
	return k
}

// Set asks for the computer to stay awake (true) or not (false).
func (k *Keeper) Set(on bool) {
	// Only the latest wish matters: replace any that's still waiting.
	select {
	case <-k.want:
	default:
	}
	select {
	case k.want <- on:
	default:
	}
}

// Close lets the computer sleep again and stops the Keeper.
func (k *Keeper) Close() {
	close(k.want)
	<-k.done
}

func (k *Keeper) run() {
	defer close(k.done)
	var release func()
	for on := range k.want {
		switch {
		case on && release == nil:
			r, err := inhibit(k.app, "Playing music")
			if err != nil {
				k.log.Warn("can't keep the screen awake", "err", err)
				continue
			}
			k.log.Info("keeping the screen awake")
			release = r
		case !on && release != nil:
			release()
			release = nil
			k.log.Info("screen may sleep again")
		}
	}
	if release != nil {
		release()
	}
}
