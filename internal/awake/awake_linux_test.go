package awake

import (
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/kyledickey/sptui/internal/logging"
)

// inhibitors asks KDE's power manager who's keeping the computer awake.
func inhibitors(t *testing.T) []string {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skip("no session bus")
	}
	defer conn.Close()
	var list [][]string
	obj := conn.Object("org.kde.Solid.PowerManagement", "/org/kde/Solid/PowerManagement/PolicyAgent")
	if err := obj.Call("org.kde.Solid.PowerManagement.PolicyAgent.ListInhibitions", 0).Store(&list); err != nil {
		t.Skip("not KDE Plasma: can't list inhibitions")
	}
	var apps []string
	for _, entry := range list {
		apps = append(apps, entry[0])
	}
	return apps
}

// waitFor polls until the inhibitor list does or doesn't include app. KDE
// only lists an inhibition after about five seconds, ignoring brief ones.
func waitFor(t *testing.T, app string, present bool) {
	t.Helper()
	for range 50 {
		if slices.Contains(inhibitors(t), app) == present {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s listed = %v, want %v (list %v)", app, !present, present, inhibitors(t))
}

func TestKeepsKDEAwake(t *testing.T) {
	if testing.Short() {
		t.Skip("takes ~10s: KDE delays inhibitions")
	}
	inhibitors(t) // skips when not on KDE
	const app = "sptui-test"
	k := New(app, logging.Discard())
	k.Set(true)
	waitFor(t, app, true)
	k.Set(false)
	waitFor(t, app, false)
	k.Set(true)
	k.Close() // releases on the way out, even if it was just taken
	time.Sleep(6 * time.Second)
	waitFor(t, app, false)
}
