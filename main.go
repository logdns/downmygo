// downmygo is a download manager for Windows, macOS and Linux, with a
// native interface drawn by MyGo. This file is the app's entry point: it
// loads the state, makes the window and the menus, and hands the rest to
// package app.
package main

import (
	"log"
	"os"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/downmygo/downmygo/internal/app"
	"github.com/downmygo/downmygo/internal/cli"
	"github.com/downmygo/downmygo/internal/store"
	"github.com/downmygo/downmygo/locales"
)

func main() {
	// `downmygo get URL` is the terminal tool, and the same binary.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "get", "download", "help", "--help", "-h", "version", "--version",
			"info", "mcp", "native-host", "native-messaging", "install-host",
			"grab", "import":
			os.Exit(cli.Main(os.Args[1:]))
		}
		if strings.HasPrefix(os.Args[1], "http://") || strings.HasPrefix(os.Args[1], "https://") {
			os.Exit(cli.Main(os.Args[1:]))
		}
	}

	dir, err := store.DataDir()
	if err != nil {
		log.Fatalf("downmygo: cannot find a data folder: %v", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		log.Fatalf("downmygo: %v", err)
	}
	state, err := st.Load()
	if err != nil {
		log.Printf("downmygo: %v", err)
	}
	a := app.New(state, st)
	a.ApplyLanguage()

	mygo.App.SetName("downmygo")
	mygo.App.SetVersion(app.Version)

	if !mygo.App.RequestSingleInstanceLock() {
		// Another copy is running; it got the link from the command line.
		return
	}

	var win *mygo.Window
	mygo.App.OnSecondInstance(func(args []string, wd string) {
		if win == nil {
			return
		}
		win.Update(func() {
			a.RevealWindow()
			a.AddFromArgs(args)
		})
	})

	mygo.App.OnOpenURL(func(raw string) {
		if win == nil {
			return
		}
		if u := linkFromDeepLink(raw); u != "" {
			win.Update(func() { a.OpenAdd(u) })
		}
	})

	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "downmygo",
			Width:     1120,
			Height:    720,
			MinWidth:  820,
			MinHeight: 520,
			StateKey:  "main",
			Content:   ui.View(a.View),
		})
		a.Attach(win)

		win.OnClose(func(e *mygo.CloseEvent) {
			if a.Quitting() {
				return
			}
			if a.Settings().CloseToTray {
				e.PreventDefault()
				win.Hide()
				return
			}
			a.Shutdown()
		})
		win.OnMinimize(func() {
			if a.Settings().MinimizeToTray {
				win.Hide()
			}
		})

		mygo.App.SetMenu(buildMenu(a))
		tray := makeTray(a, win)

		// What the app set once, as the menu's labels and the tray's,
		// follows the language when the user changes it.
		mygo.App.OnLocaleChanged(func(string) {
			win.SetTitle("downmygo")
			mygo.App.SetMenu(buildMenu(a))
			if tray != nil {
				tray.SetMenu(trayMenu(a))
			}
		})

		if a.Settings().RemoteEnabled {
			if err := a.StartAPI(true); err != nil {
				log.Printf("downmygo: remote control: %v", err)
			}
		}
		go a.WatchClipboard()

		a.AddFromArgs(os.Args[1:])
	})

	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		a.Shutdown()
	})

	mygo.Theme.OnUpdated(func() {
		if win != nil {
			win.Invalidate()
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func buildMenu(a *app.App) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{
			Label: "downmygo",
			Submenu: []*mygo.MenuItem{
				{Label: locales.MenuAbout(), Click: func(*mygo.MenuItem, *mygo.Window) { a.ShowAbout() }},
				mygo.Separator(),
				{Label: locales.MenuOptions(), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.ShowOptions() }},
				{Label: locales.MenuCheckUpdates(), Click: func(*mygo.MenuItem, *mygo.Window) { a.CheckForUpdates() }},
				mygo.Separator(),
				{Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers},
				{Role: mygo.RoleUnhide},
				mygo.Separator(),
				{Label: locales.MenuQuit(), Accelerator: "CmdOrCtrl+Q", Click: func(*mygo.MenuItem, *mygo.Window) { a.Quit() }},
			},
		},
		{
			Label: locales.MenuFile(),
			Submenu: []*mygo.MenuItem{
				{Label: locales.MenuAddURL(), Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) { a.OpenAdd("") }},
				{Label: locales.MenuAddTorrent(), Disabled: true},
				mygo.Separator(),
				{Label: locales.MenuPasteLinks(), Accelerator: "CmdOrCtrl+Shift+V", Click: func(*mygo.MenuItem, *mygo.Window) {
					a.PasteLinks()
				}},
				mygo.Separator(),
				{Label: locales.MenuGrab(), Click: func(*mygo.MenuItem, *mygo.Window) { a.GrabDialog() }},
				{Label: locales.MenuImport(), Click: func(*mygo.MenuItem, *mygo.Window) { a.ImportDialog() }},
				{Label: locales.MenuFeeds(), Click: func(*mygo.MenuItem, *mygo.Window) { a.ShowFeeds() }},
				mygo.Separator(),
				{Label: locales.MenuCloseWindow(), Role: mygo.RoleClose, Click: func(_ *mygo.MenuItem, w *mygo.Window) {
					if w != nil {
						w.Close()
					}
				}},
			},
		},
		{Role: mygo.RoleEditMenu},
		{
			Label: locales.MenuDownloads(),
			Submenu: []*mygo.MenuItem{
				{Label: locales.MenuPauseAll(), Click: func(*mygo.MenuItem, *mygo.Window) { a.PauseAll() }},
				{Label: locales.MenuResumeAll(), Click: func(*mygo.MenuItem, *mygo.Window) { a.ResumeAll() }},
				mygo.Separator(),
				{Label: locales.MenuStartMain(), Click: func(*mygo.MenuItem, *mygo.Window) { a.StartQueue("main") }},
				{Label: locales.MenuStopMain(), Click: func(*mygo.MenuItem, *mygo.Window) { a.StopQueue("main") }},
				mygo.Separator(),
				{Label: locales.MenuOptions(), Click: func(*mygo.MenuItem, *mygo.Window) { a.ShowOptions() }},
			},
		},
		{
			Label: locales.MenuView(),
			Submenu: []*mygo.MenuItem{
				{Label: locales.MenuLight(), Click: func(*mygo.MenuItem, *mygo.Window) { a.SetTheme("light") }},
				{Label: locales.MenuDark(), Click: func(*mygo.MenuItem, *mygo.Window) { a.SetTheme("dark") }},
				{Label: locales.MenuSystem(), Click: func(*mygo.MenuItem, *mygo.Window) { a.SetTheme("system") }},
				mygo.Separator(),
				{Role: mygo.RoleToggleFullScreen},
			},
		},
		{
			Label: locales.MenuHelp(),
			Submenu: []*mygo.MenuItem{
				{Label: locales.MenuWebsite(), Click: func(*mygo.MenuItem, *mygo.Window) {
					_ = mygo.Shell.OpenExternal("https://downmygo.app")
				}},
				{Label: locales.MenuShortcuts(), Click: func(*mygo.MenuItem, *mygo.Window) { a.ShowAbout() }},
			},
		},
	})
}

// trayMenu is the menu bar item's menu, made again when the language
// changes.
func trayMenu(a *app.App) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: locales.TrayShow(), Click: func(*mygo.MenuItem, *mygo.Window) { a.RevealWindow() }},
		mygo.Separator(),
		{Label: locales.MenuPauseAll(), Click: func(*mygo.MenuItem, *mygo.Window) { a.PauseAll() }},
		{Label: locales.MenuResumeAll(), Click: func(*mygo.MenuItem, *mygo.Window) { a.ResumeAll() }},
		mygo.Separator(),
		{Label: locales.TrayQuit(), Click: func(*mygo.MenuItem, *mygo.Window) { a.Quit() }},
	})
}

func makeTray(a *app.App, win *mygo.Window) *mygo.Tray {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID: "downmygo",
		// The menu bar tints a template image itself, so the mark reads in
		// both appearances.
		Icon:           app.TrayPNG(32),
		IconIsTemplate: true,
		ToolTip:        "downmygo",
		Menu:           trayMenu(a),
	})
	if err != nil {
		log.Printf("downmygo: tray: %v", err)
		return nil
	}
	tray.OnClick(func() { a.RevealWindow() })
	return tray
}

// linkFromDeepLink reads downmygo://add?url=… and downmygo://<url>.
func linkFromDeepLink(raw string) string {
	const scheme = "downmygo://"
	if !strings.HasPrefix(raw, scheme) {
		return ""
	}
	rest := strings.TrimPrefix(raw, scheme)
	if strings.HasPrefix(rest, "add?") {
		for _, part := range strings.Split(strings.TrimPrefix(rest, "add?"), "&") {
			if strings.HasPrefix(part, "url=") {
				u := strings.TrimPrefix(part, "url=")
				if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
					return u
				}
			}
		}
		return ""
	}
	if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
		return rest
	}
	return ""
}
