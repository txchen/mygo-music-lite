package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"mygo-music/internal/i18n"
)

// settings are the listener's choices, kept in settings.json in the user
// data directory.
type settings struct {
	// Language is what the app speaks: i18n.English or i18n.Chinese, or
	// i18n.System for the system's.
	Language string `json:"language"`
	// Source is the ID of the source last used.
	Source string `json:"source,omitempty"`
	// DisabledPlugins are the IDs of the plugins turned off. All are on until
	// then, the built-in one included.
	DisabledPlugins []string `json:"disabledPlugins,omitempty"`
	// PluginsChecked is when plugins were last checked for new versions, once a
	// day, and PluginUpdates the versions found, by plugin.
	PluginsChecked time.Time         `json:"pluginsChecked,omitzero"`
	PluginUpdates  map[string]string `json:"pluginUpdates,omitempty"`
	// Notch shows the quick player around the notch: the listener turns
	// it on.
	Notch bool `json:"notch"`
	// NotchActivity shows the cover and the music's levels beside the
	// notch while music plays, which hides the menu bar there.
	NotchActivity bool `json:"notchActivity"`
	// NotchVirtual keeps a notch of the app's own showing at the top of a
	// display without one; else it shows only while the pointer asks.
	NotchVirtual bool `json:"notchVirtual"`
	// NotchGreeted says the notch showed itself, once, as the first song
	// played (greet).
	NotchGreeted bool `json:"notchGreeted"`
	// Quality is what's asked of sources: standard, high or lossless. A source
	// may ignore it.
	Quality string `json:"quality"`
	// Appearance is light, dark, or system to follow the system.
	Appearance string `json:"appearance"`
	// GrowFromCover opens Now Playing out of the player bar's cover, and
	// closes it back into it: an experiment the listener turns on. Off, it
	// slides up from the bottom of the window.
	GrowFromCover bool `json:"growFromCover"`
	// Volume is where the volume was left, from 0 to 1.
	Volume float64 `json:"volume"`
	// HotComments shows a song's top comments in its quiet parts, in place of
	// the lyrics (quietComment), for a source with comments: the listener
	// turns it on.
	HotComments bool `json:"hotComments"`
	// AppIcon is where the app's icon shows on macOS (appIcons); elsewhere
	// it is always in the notification area.
	AppIcon string `json:"appIcon"`
	// SearchHistory keeps submitted keywords, most recent first.
	SearchHistory []string `json:"searchHistory,omitempty"`

	path string // where they were loaded from, and are saved to
}

func defaultSettings() settings {
	return settings{Language: i18n.System, Quality: "lossless", Appearance: "system", Volume: 0.8, AppIcon: "dock-and-menubar"}
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// loadSettings reads the settings, or returns the defaults.
func loadSettings() settings {
	s := defaultSettings()
	if path, err := settingsPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, &s)
		}
		s.path = path
	}
	return s
}

// save keeps the settings where they were loaded from; defaults that were
// never loaded, as in tests, are not saved.
func (s settings) save() error {
	path := s.path
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// applyAppearance makes the windows light, dark, or follow the system. Only
// a new one: it repaints every window, waiting for the main thread, and any
// setting changed applies it.
func (s settings) applyAppearance() {
	src := mygo.ThemeSystem
	switch s.Appearance {
	case "light":
		src = mygo.ThemeLight
	case "dark":
		src = mygo.ThemeDark
	}
	if src != mygo.Theme.Source() {
		mygo.Theme.SetSource(src)
	}
}

// qualityLevels are what can be asked of sources, by what they sound like.
var qualityLevels = []struct{ level, label string }{
	{"standard", N("Standard")},
	{"high", N("High")},
	{"lossless", N("Lossless")},
}
