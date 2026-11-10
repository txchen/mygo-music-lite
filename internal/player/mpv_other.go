//go:build !darwin || !arm64

package player

import (
	"errors"
	"mygo-music/internal/music"
)

func openMPV(*music.Stream) (engine, error) {
	return nil, errors.New("optional libmpv fallback is available only on macOS Apple Silicon")
}
