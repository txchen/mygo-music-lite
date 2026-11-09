package player

import (
	"fmt"
	"os"
	"testing"

	"github.com/egoist/mygo"
)

// Native readiness and seek completions require the macOS application run loop.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mygo-lite-player-test-")
	if err != nil {
		panic(err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	mygo.App.SetPath(mygo.PathCache, dir)
	result := make(chan int, 1)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		// A headless test app must not terminate while a native test is active.
		if len(result) == 0 {
			e.PreventDefault()
		}
	})
	mygo.App.WhenReady(func() {
		go func() {
			result <- m.Run()
			mygo.RunOnMain(mygo.App.Quit)
		}()
	})
	err = mygo.App.Run()
	os.RemoveAll(dir)
	select {
	case code := <-result:
		os.Exit(code)
	default:
		fmt.Fprintf(os.Stderr, "native event loop stopped before tests completed: %v\n", err)
		os.Exit(1)
	}
}
