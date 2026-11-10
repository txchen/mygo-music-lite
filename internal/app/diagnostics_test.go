package app

import (
	"github.com/egoist/mygo/ui"
	"mygo-music/internal/diagnostics"
	"testing"
)

// The logging switch starts recording and stops without losing collected evidence.
func TestDiagnosticSwitch(t *testing.T) {
	diagnostics.Set(false)
	defer diagnostics.Set(false)
	a := newTestApp()
	a.settingsPage = "playback"
	tt := ui.NewTester(a.settingsView, 780, 1000)
	tt.Frame()
	if err := tt.Click("Enable Playback Diagnostic Log"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !diagnostics.Enabled() {
		t.Fatal("switch did not start logging")
	}
	if err := tt.Click("Enable Playback Diagnostic Log"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if diagnostics.Enabled() {
		t.Fatal("switch did not stop logging")
	}
}
