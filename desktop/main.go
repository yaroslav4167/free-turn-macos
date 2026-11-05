package main

import (
	"embed"
	"fmt"
	"os"
	"strconv"

	appassets "github.com/samosvalishe/free-turn-proxy/desktop/assets"
	"github.com/samosvalishe/free-turn-proxy/desktop/backend/vpnservice"
	"github.com/samosvalishe/free-turn-proxy/desktop/backend/wintunnel"
	"github.com/samosvalishe/free-turn-proxy/desktop/integration"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if integration.HandleUpdateProcess() {
		return
	}
	if len(os.Args) >= 2 && (os.Args[1] == "--install-vpn-service" || os.Args[1] == "--remove-vpn-service") {
		var err error
		if os.Args[1] == "--install-vpn-service" && len(os.Args) == 4 {
			var uid uint64
			uid, err = strconv.ParseUint(os.Args[3], 10, 32)
			if err == nil {
				err = vpnservice.Install(uint32(uid), os.Args[2])
			}
		} else if os.Args[1] == "--remove-vpn-service" && len(os.Args) == 3 {
			var uid uint64
			uid, err = strconv.ParseUint(os.Args[2], 10, 32)
			if err == nil {
				err = vpnservice.Remove(uint32(uid))
			}
		} else {
			err = fmt.Errorf("invalid VPN service installation arguments")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--install-desktop" {
		path, err := integration.Install()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("FreeTurn installed:", path)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--create-desktop-shortcut" {
		path, err := integration.CreateDesktopShortcut()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("FreeTurn desktop shortcut:", path)
		return
	}
	if len(os.Args) == 2 && (os.Args[1] == "--configure-vpn-helper" || os.Args[1] == "--uninstall-desktop") {
		var err error
		if os.Args[1] == "--configure-vpn-helper" {
			err = integration.ConfigureVPNHelper(false)
		} else {
			err = integration.Uninstall()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--vpn-helper" {
		if err := wintunnel.RunHelper(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// Create an instance of the app structure
	rendering := configureRendering(os.Args[1:])
	app := NewApp()
	app.renderingNote = rendering.note

	// Create application with options
	err := wails.Run(&options.App{
		Title:             "FreeTurn",
		Menu:              applicationMenu(),
		Mac:               &mac.Options{About: &mac.AboutInfo{Title: "FreeTurn", Message: "FreeTurn for macOS", Icon: appassets.IconPNG}},
		Width:             900,
		Height:            600,
		MinWidth:          900,
		MinHeight:         600,
		HideWindowOnClose: false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 14, G: 15, B: 20, A: 255},
		Linux: &linux.Options{
			Icon:             appassets.IconPNG,
			ProgramName:      "freeturn",
			WebviewGpuPolicy: rendering.policy,
		},
		OnStartup:     app.startup,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		OnDomReady:    app.domReady,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "9fbdd69c-731b-4ab9-91d4-c94e6814b335",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) { app.ShowWindow() },
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

func applicationMenu() *menu.Menu {
	result := menu.NewMenu()
	result.Append(menu.AppMenu())
	result.Append(menu.EditMenu())
	return result
}
