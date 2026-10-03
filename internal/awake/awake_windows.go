package awake

import (
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// inhibit asks Windows to keep the display and system awake. The request
// belongs to the thread that makes it, so a goroutine holds a thread of its
// own until it's released.
func inhibit(_, _ string) (func(), error) {
	errc := make(chan error, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if r, _, err := setThreadExecutionState.Call(esContinuous | esSystemRequired | esDisplayRequired); r == 0 {
			errc <- err
			return
		}
		errc <- nil
		<-stop
		setThreadExecutionState.Call(esContinuous)
	}()
	if err := <-errc; err != nil {
		return nil, err
	}
	return func() {
		close(stop)
		<-done
	}, nil
}
