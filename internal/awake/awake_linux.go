package awake

import (
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// inhibit asks the desktop over D-Bus. KDE and most desktops speak the
// freedesktop ScreenSaver (screen on, no lock) and PowerManagement (no idle
// sleep) interfaces; GNOME has its own session manager call covering both.
// The inhibitions belong to our bus connection, so they end even if sptui
// crashes.
func inhibit(app, reason string) (func(), error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect to the session bus: %w", err)
	}
	var undo []func()
	try := func(dest, path, iface, uninhibit string, args ...any) error {
		obj := conn.Object(dest, dbus.ObjectPath(path))
		var cookie uint32
		if err := obj.Call(iface+".Inhibit", 0, args...).Store(&cookie); err != nil {
			return err
		}
		undo = append(undo, func() { obj.Call(iface+"."+uninhibit, 0, cookie) })
		return nil
	}

	errScreen := try("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver",
		"org.freedesktop.ScreenSaver", "UnInhibit", app, reason)
	errSleep := try("org.freedesktop.PowerManagement", "/org/freedesktop/PowerManagement/Inhibit",
		"org.freedesktop.PowerManagement.Inhibit", "UnInhibit", app, reason)
	if len(undo) == 0 {
		// GNOME: flags 4 (suspend) | 8 (idle); 0 as there's no window ID.
		errGnome := try("org.gnome.SessionManager", "/org/gnome/SessionManager",
			"org.gnome.SessionManager", "Uninhibit", app, uint32(0), reason, uint32(4|8))
		if errGnome != nil {
			conn.Close()
			return nil, errors.Join(errScreen, errSleep, errGnome)
		}
	}
	return func() {
		for _, u := range undo {
			u()
		}
		conn.Close()
	}, nil
}
