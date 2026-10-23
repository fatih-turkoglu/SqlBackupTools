package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"sqlbackup/internal/app"
	"sqlbackup/internal/cli"
)

//go:embed all:frontend/dist
var assets embed.FS

// version, build sırasında -ldflags "-X main.version=1.2.3" ile verilir.
var version = "dev"

func main() {
	// Argüman varsa komut satırı modu (penceresiz), yoksa arayüz.
	if len(os.Args) > 1 {
		cli.AttachConsole()
		os.Exit(cli.Main(os.Args[1:], version))
	}

	// Arayüz: app paketinin dışa açık yöntemleri frontend'e bağlanır.
	a := app.New(version)
	err := wails.Run(&options.App{
		Title:  "SqlBackup",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.Startup(a),
		OnShutdown:       app.Shutdown(a),
		Bind:             []interface{}{a},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
