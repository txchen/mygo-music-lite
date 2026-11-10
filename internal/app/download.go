package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-music/internal/local"
	"mygo-music/internal/music"
)

// saveSongAs asks the listener where to save a song with the native dialog,
// suggesting name in their Downloads folder, and returns the path, or "" if
// they didn't pick one. It blocks, so it runs on a goroutine; tests replace it.
var saveSongAs = func(a *app, name string) (string, error) {
	dir, _ := mygo.App.Path(mygo.PathDownloads)
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:            a.win,
		Title:             T("Download"),
		DefaultPath:       filepath.Join(dir, name),
		ButtonLabel:       T("Download"),
		CreateDirectories: true,
	})
	return path, err
}

// canDownload reports whether tr can be saved to a file of the listener's: a
// song its source streams for the app to play, rather than one already on disk
// or one that plays elsewhere.
func (a *app) canDownload(tr *music.Track) bool {
	lib := a.trackLib(tr)
	return lib != nil && lib.id != local.ID && lib.can("stream") &&
		lib.src.Info().Playback == music.PlaybackStream && !tr.Unavailable
}

// download saves a copy of tr where the listener picks, at the quality the
// settings say, from the cache if it's kept there; toasts say how it goes.
func (a *app) download(tr music.Track) {
	lib := a.trackLib(&tr)
	if lib == nil {
		return
	}
	a.downloadSerial++
	noticeID := fmt.Sprintf("download-%d", a.downloadSerial)
	fail := func(err error) {
		a.post(func() {
			a.downloadNotices = append(a.downloadNotices, ui.Toast{ID: noticeID, Title: T("Couldn't download this song"), Description: err.Error(), Type: "error", Timeout: -1})
		})
	}
	quality := a.settings.Quality
	go func() {
		song, err := a.pl.FindSong(lib.src, tr, quality)
		if err != nil {
			fail(err)
			return
		}
		path, err := saveSongAs(a, songFileName(&tr, song.Type))
		if err != nil {
			song.Close()
			fail(err)
			return
		}
		if path == "" {
			song.Close()
			return
		}
		a.post(func() {
			a.downloadNotices = append(a.downloadNotices, ui.Toast{ID: noticeID, Title: T("Downloading…"), Description: tr.Name, Timeout: -1})
		})
		err = song.SaveTo(path)
		if err != nil {
			fail(err)
			return
		}
		a.post(func() {
			a.downloadNotices = append(a.downloadNotices, ui.Toast{ID: noticeID, Title: Tf("Downloaded %s", tr.Name), Type: "success", Timeout: 10 * time.Second, Action: T("Show in Folder"), OnAction: func() { mygo.Shell.ShowItemInFolder(path) }})
		})
	}()
}

// songFileName names the file of a song of type ext as its credits read,
// "Artists - Title.ext", with what no file name may hold taken out.
func songFileName(tr *music.Track, ext string) string {
	name := tr.Name
	if artists := tr.ArtistNames(); artists != "" {
		name = artists + " - " + name
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	// Most systems take 255 bytes, with room left for the extension.
	for len(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	// Windows drops a trailing dot or space, and a leading dot hides the file.
	name = strings.Trim(name, ". ")
	if name == "" {
		name = T("Song")
	}
	return name + "." + ext
}
