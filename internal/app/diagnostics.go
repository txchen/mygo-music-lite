package app

import (
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"mygo-music/internal/diagnostics"
)

func (a *app) exportDiagnosticLog() {
	parent, version := a.settingsWin, mygo.App.Version()
	title := T("Export Diagnostic Log")
	go func() {
		dir, _ := mygo.App.Path(mygo.PathDownloads)
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: parent, Title: title,
			DefaultPath: filepath.Join(dir, "mygo-playback-"+time.Now().Format("20060102-150405")+".json"),
		})
		if path == "" && err == nil {
			return
		}
		if err == nil {
			var data []byte
			data, err = diagnostics.Snapshot(version)
			if err == nil {
				err = os.WriteFile(path, data, 0600)
			}
		}
		a.post(func() {
			if err != nil {
				a.toast = T("Couldn't export diagnostic log")
			} else {
				a.toast = T("Diagnostic log exported")
				mygo.Shell.ShowItemInFolder(path)
			}
		})
	}()
}
