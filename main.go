package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"log"

	"mygo-music/internal/app"
)

func main() {
	mygo.Use(native.Plugin)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
