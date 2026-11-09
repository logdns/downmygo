package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/downmygo/downmygo/internal/engine"
	"github.com/downmygo/downmygo/internal/model"
	brand "github.com/downmygo/downmygo/internal/ui"
	"github.com/downmygo/downmygo/locales"
)

// colKind names a column of the list, so the layout can leave some out
// when the window is narrow without moving the others' meaning.
type colKind int

const (
	colName colKind = iota
	colSize
	colProgress
	colSpeed
	colStatus
	colAdded
)

// View builds the window. MyGo calls it on the main thread after input,
// after an engine event, and while something animates.
func (a *App) View(c *ui.Context) {
	dark := false
	if mygo.App.IsReady() {
		dark = mygo.Theme.IsDark()
	}
	switch a.st.Settings.Theme {
	case "light":
		dark = false
	case "dark":
		dark = true
	}
	t := brand.Theme(dark, a.st.Settings.Accent)
	c.SetTheme(t)

	a.viewList = a.visible()
	if a.selected >= len(a.viewList) {
		a.selected = len(a.viewList) - 1
	}
	if a.selected < 0 {
		a.selected = 0
	}

	w, h := a.windowSize()
	a.compact = w < 820
	a.narrow = w < 1060
	// The clipboard watcher runs on its own goroutine; it reads this.
	a.clipWatch.Store(a.st.Settings.ClipboardWatch)

	a.shortcuts(c)

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.toolbar(c, t, w)
		if a.compact {
			a.mainPane(c, t, w, h)
		} else {
			ui.Split(c, &a.sidebarW, func() {
				a.sidebar(c, t)
			}, func() {
				a.mainPane(c, t, w, h)
			}).Fill().SplitReset(236)
		}
		a.statusBar(c, t, w)
	})

	a.dialogs(c, t, w, h)

	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
}

// SetPreviewSize tells the app the size a windowless render draws in, so
// cmd/preview shows the layout a window of that size gets.
func (a *App) SetPreviewSize(w, h int) {
	a.previewW, a.previewH = w, h
}

// windowSize is the content area in points, or a sensible default before
// the window exists.
func (a *App) windowSize() (int, int) {
	if a.win == nil {
		if a.previewW > 0 && a.previewH > 0 {
			return a.previewW, a.previewH
		}
		return 1120, 720
	}
	w, h := a.win.ContentSize()
	if w <= 0 || h <= 0 {
		return 1120, 720
	}
	return w, h
}

func (a *App) shortcuts(c *ui.Context) {
	c.OnWindowShortcut(ui.Cmd, ui.KeyN, func() { a.OpenAdd("") })
	c.OnWindowShortcut(ui.Cmd, ui.KeyV, func() {
		if text := strings.TrimSpace(c.ReadClipboard()); looksLikeURL(text) {
			a.OpenAdd(text)
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyF, func() { a.searchFocus = true })
	c.OnWindowShortcut(ui.Cmd, ui.KeyO, func() { a.OpenAdd("") })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyO, func() { a.optionsOpen = true })
	c.OnWindowShortcut(ui.Cmd, ui.KeyComma, func() { a.optionsOpen = true })
	c.OnWindowShortcut(ui.Cmd, ui.KeyA, func() {
		if len(a.viewList) > 0 {
			a.selected = 0
		}
	})
	c.OnWindowShortcut(0, ui.KeySpace, func() {
		if d := a.SelectedDownload(); d != nil {
			if d.Status.Active() {
				a.Pause(d.ID)
			} else {
				a.Resume(d.ID)
			}
		}
	})
	c.OnWindowShortcut(0, ui.KeyDelete, func() {
		if d := a.SelectedDownload(); d != nil {
			a.askRemove(d, false)
		}
	})
	c.OnWindowShortcut(0, ui.KeyEnter, func() {
		if d := a.SelectedDownload(); d != nil {
			if d.Status == model.StatusComplete {
				a.OpenFile(d)
			} else {
				a.showDetail = true
			}
		}
	})
}

func (a *App) toolbar(c *ui.Context, t *ui.Theme, w int) {
	ui.Toolbar(c, func() {
		// Brand mark.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 6, 0, 2).Children(func() {
			a.mark(c, 24, 7, 14)
			if w >= 700 {
				ui.Text(c, "downmygo").Bold().FontSize(14)
			}
		})
		ui.Divider(c).Height(18).Margin(4, 8)

		if w >= 860 {
			if ui.PrimaryButton(c, locales.AddURL()).Clicked() {
				a.OpenAdd("")
			}
		} else if a.iconButton(c, t, brand.Plus, locales.AddURL(), func() {
			a.OpenAdd("")
		}).Clicked() {
		}
		a.iconButton(c, t, brand.Play, locales.Resume(), func() {
			if d := a.SelectedDownload(); d != nil {
				a.Resume(d.ID)
			}
		})
		a.iconButton(c, t, brand.Pause, locales.Pause(), func() {
			if d := a.SelectedDownload(); d != nil {
				a.Pause(d.ID)
			}
		})
		a.iconButton(c, t, brand.Trash, locales.Remove(), func() {
			if d := a.SelectedDownload(); d != nil {
				a.askRemove(d, false)
			}
		})
		if w >= 760 {
			a.iconButton(c, t, brand.Folder, locales.OpenFolder(), func() {
				if d := a.SelectedDownload(); d != nil {
					a.OpenFolder(d)
				}
			})
		}

		ui.MenuButton(c, locales.Tasks(), func(m *ui.Menu) {
			if m.Item(locales.TasksPaste()).Chosen() {
				for _, line := range strings.Split(mygo.Clipboard.ReadText(), "\n") {
					if looksLikeURL(strings.TrimSpace(line)) {
						a.AddURL(strings.TrimSpace(line))
					}
				}
			}
			if m.Item(locales.TasksPauseAll()).Chosen() {
				a.PauseAll()
			}
			if m.Item(locales.TasksResumeAll()).Chosen() {
				a.ResumeAll()
			}
			m.Separator()
			if m.Item(locales.TasksSoon()).Disabled(true).Chosen() {
			}
		})

		ui.Spacer(c)

		searchW := float32(220)
		if w < 900 {
			searchW = 160
		}
		if w >= 640 {
			search := ui.SearchField(c, &a.query).Width(searchW).Label(locales.SearchDownloads())
			if a.searchFocus {
				search.AutoFocus()
				a.searchFocus = false
			}
		}
		a.iconButton(c, t, brand.Settings, locales.Options(), func() { a.optionsOpen = true })
	}).Padding(6, 12).BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
}

// mark draws the app's picture in a tile of the brand's gradient.
func (a *App) mark(c *ui.Context, size, radius float32, iconSize float32) ui.Element {
	return ui.Box(c).Size(size, size).Radius(radius).
		Gradient(ui.Hex("#4A78FF"), ui.Hex("#0FA79A"), 135).Center().
		Children(func() {
			ui.Icon(c, brand.Mark).FontSize(iconSize).TextColor(ui.Hex("#FFFFFF"))
		})
}

func (a *App) iconButton(c *ui.Context, t *ui.Theme, icon *ui.SVG, label string, fn func()) ui.Element {
	e := ui.ButtonBase(c).Padding(7).Radius(9).Label(label).Tooltip(label)
	if e.Hovered() {
		e.Background(t.SurfaceHover)
	}
	e.Children(func() {
		ui.Icon(c, icon).FontSize(17)
	})
	if e.Clicked() {
		fn()
	}
	return e
}

func (a *App) sidebar(c *ui.Context, t *ui.Theme) {
	counts := map[string]int{}
	for _, d := range a.st.Downloads {
		counts["all"]++
		if d.Status == model.StatusComplete {
			counts["finished"]++
		} else {
			counts["unfinished"]++
		}
		counts["cat:"+d.Category]++
		if d.Queue != "" {
			counts["queue:"+d.Queue]++
		}
	}

	ui.Box(c).FillHeight().Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		if ui.Sidebar(c, &a.sidebarSel, func() {
			ui.SidebarSection(c, locales.SidebarDownloads(), nil, func() {
				ui.SidebarItem(c, "all", brand.Download, locales.SidebarAll()).Children(func() {
					ui.Badge(c, fmt.Sprint(counts["all"]))
				})
				ui.SidebarItem(c, "unfinished", brand.Clock, locales.SidebarUnfinished()).Children(func() {
					ui.Badge(c, fmt.Sprint(counts["unfinished"]))
				})
				ui.SidebarItem(c, "finished", brand.Check, locales.SidebarFinished()).Children(func() {
					ui.Badge(c, fmt.Sprint(counts["finished"]))
				})
			})
			ui.SidebarSection(c, locales.SidebarCategories(), nil, func() {
				for _, cat := range a.st.Categories {
					id := "cat:" + cat.ID
					ui.SidebarItem(c, id, categoryIcon(cat), categoryName(cat)).Children(func() {
						if n := counts[id]; n > 0 {
							ui.Badge(c, fmt.Sprint(n))
						}
					})
				}
			})
			ui.SidebarSection(c, locales.SidebarQueues(), nil, func() {
				for _, q := range a.st.Queues {
					id := "queue:" + q.ID
					item := ui.SidebarItem(c, id, brand.Queue, queueName(q))
					item.Children(func() {
						if a.queueRunning[q.ID] {
							ui.Box(c).Size(7, 7).Radius(99).Background(t.Success)
						}
						if n := counts[id]; n > 0 {
							ui.Badge(c, fmt.Sprint(n))
						}
					})
				}
			})
		}).Changed() {
			a.SetFilter(a.sidebarSel)
		}
	})
}

func categoryIcon(cat model.Category) *ui.SVG {
	switch cat.ID {
	case "music":
		return brand.Bolt
	case "video":
		return brand.Play
	case "compressed":
		return brand.Queue
	case "documents":
		return brand.File
	case "programs":
		return brand.Settings
	case "torrents":
		return brand.Link
	}
	return brand.Folder
}

// filterMenu lists every filter, for a window too narrow for the sidebar.
func (a *App) filterMenu(m *ui.Menu) {
	item := func(id, name string) {
		if m.Item(name).Radio(a.filter == id || (id == "all" && a.filter == "")).Chosen() {
			a.SetFilter(id)
		}
	}
	item("all", locales.SidebarAll())
	item("unfinished", locales.SidebarUnfinished())
	item("finished", locales.SidebarFinished())
	m.Separator()
	for _, cat := range a.st.Categories {
		item("cat:"+cat.ID, categoryName(cat))
	}
	if len(a.st.Queues) > 0 {
		m.Separator()
		for _, q := range a.st.Queues {
			item("queue:"+q.ID, queueName(q))
		}
	}
}

func (a *App) mainPane(c *ui.Context, t *ui.Theme, w, h int) {
	ui.Column(c).Fill().Children(func() {
		// Header.
		ui.Row(c).Padding(14, 16, 10, 16).Gap(10).AlignItems(ui.Center).Children(func() {
			if a.compact {
				ui.MenuButton(c, a.filterTitle(), func(m *ui.Menu) { a.filterMenu(m) })
			} else {
				ui.Text(c, a.filterTitle()).FontSize(17).Bold()
			}
			ui.Text(c, fmt.Sprint(len(a.viewList))).FontSize(13).TextColor(t.TextMuted)
			ui.Spacer(c)
			ui.MenuButton(c, locales.Sort(), func(m *ui.Menu) {
				for i, name := range a.sortNames() {
					item := m.Item(name).Radio(a.sortCol == i)
					if item.Chosen() {
						a.sortCol = i
						a.sortDesc = false
					}
				}
				m.Separator()
				if m.Item(locales.SortDescending()).Checked(a.sortDesc).Chosen() {
					a.sortDesc = !a.sortDesc
				}
			})
			toggle := a.showDetail
			if ui.Toggle(c, &toggle, locales.Details()).Changed() {
				a.showDetail = toggle
			}
		})

		if len(a.viewList) == 0 {
			a.emptyState(c, t)
			return
		}

		a.listState.Key = func(row int) any {
			if row >= 0 && row < len(a.viewList) {
				return a.viewList[row].ID
			}
			return row
		}
		a.listState.Selected = &a.selected
		a.listState.CellMenu = func(m *ui.Menu, row, col int) {
			if row < 0 || row >= len(a.viewList) {
				return
			}
			a.rowMenu(m, a.viewList[row])
		}
		a.listState.HeaderMenu = func(m *ui.Menu, col int) {
			for i, name := range a.sortNames() {
				if m.Item(locales.SortBy(name)).Radio(a.sortCol == i).Chosen() {
					a.sortCol = i
					a.sortDesc = false
				}
			}
			m.Separator()
			if m.Item(locales.SortDescending()).Checked(a.sortDesc).Chosen() {
				a.sortDesc = !a.sortDesc
			}
		}

		a.columns = a.visibleColumns(w)
		cols := make([]ui.TableColumn, len(a.columns))
		for i, kind := range a.columns {
			cols[i] = a.column(kind)
		}
		tableFn := func() {
			table := ui.Table(c, &a.listState, cols, len(a.viewList), func(row, col int) {
				a.cell(c, t, row, col)
			}).Grow(1)
			if table.Submitted() {
				if d := a.SelectedDownload(); d != nil {
					if d.Status == model.StatusComplete {
						a.OpenFile(d)
					} else {
						a.showDetail = true
					}
				}
			}
		}
		if d := a.SelectedDownload(); d != nil && a.showDetail {
			height := 208
			if h < 620 {
				height = 176
			}
			ui.SplitVertical(c, &a.detailH, tableFn, func() {
				a.detail(c, t, d)
			}).SplitReset(float32(height))
		} else {
			tableFn()
		}
	})
}

// sortNames are the columns the list can be sorted by, in the order the
// menus offer them.
func (a *App) sortNames() []string {
	return []string{
		locales.SortName(), locales.SortSize(), locales.SortProgress(),
		locales.SortSpeed(), locales.SortStatus(), locales.SortAdded(),
	}
}

// visibleColumns leaves out the columns a narrow window has no room for.
func (a *App) visibleColumns(w int) []colKind {
	cols := []colKind{colName}
	if w >= 620 {
		cols = append(cols, colSize)
	}
	cols = append(cols, colProgress)
	if w >= 880 {
		cols = append(cols, colSpeed)
	}
	if w >= 700 {
		cols = append(cols, colStatus)
	}
	if w >= 1120 {
		cols = append(cols, colAdded)
	}
	return cols
}

func (a *App) column(kind colKind) ui.TableColumn {
	switch kind {
	case colSize:
		return ui.TableColumn{Title: locales.SortSize(), Width: 88, Align: ui.End}
	case colProgress:
		return ui.TableColumn{Title: locales.SortProgress(), Width: 150}
	case colSpeed:
		return ui.TableColumn{Title: locales.SortSpeed(), Width: 96, Align: ui.End}
	case colStatus:
		return ui.TableColumn{Title: locales.SortStatus(), Width: 108}
	case colAdded:
		return ui.TableColumn{Title: locales.SortAdded(), Width: 112, Align: ui.End}
	}
	return ui.TableColumn{Title: locales.SortName(), Width: 0, MinWidth: 180}
}

func (a *App) emptyState(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Center().Gap(14).Padding(40).Children(func() {
		ui.Box(c).Size(64, 64).Radius(20).
			Gradient(ui.Hex("#4A78FF"), ui.Hex("#0FA79A"), 135).Center().
			Shadow(0, 10, 30, 0, ui.RGBA(59, 110, 246, 0.25)).
			Children(func() {
				ui.Icon(c, brand.Mark).FontSize(32).TextColor(ui.Hex("#FFFFFF"))
			})
		ui.Text(c, locales.EmptyTitle()).FontSize(18).Bold()
		ui.Text(c, locales.EmptyBody()).
			TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(360)
		if ui.PrimaryButton(c, locales.EmptyAdd()).Clicked() {
			a.OpenAdd("")
		}
		ui.Text(c, locales.EmptyTip()).
			FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(360)
	})
}

func (a *App) cell(c *ui.Context, t *ui.Theme, row, col int) {
	if row < 0 || row >= len(a.viewList) || col < 0 || col >= len(a.columns) {
		return
	}
	d := a.viewList[row]
	selected := a.selected == row
	text := t.Text
	muted := t.TextMuted
	status := a.statusColor(t, d)
	if selected {
		text = t.AccentText
		muted = t.AccentText.Alpha(0.78)
		status = t.AccentText
	}
	switch a.columns[col] {
	case colName:
		ui.Row(c).Gap(9).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(8, 8).Radius(99).Background(status)
			ui.Icon(c, kindIcon(d)).FontSize(15).TextColor(muted)
			ui.Column(c).Gap(1).Children(func() {
				ui.Text(c, d.FileName).SingleLine().TextColor(text)
				sub := d.Category
				if cat := a.category(d.Category); cat != nil {
					sub = categoryName(*cat)
				}
				if d.Queue != "" {
					if q := a.queue(d.Queue); q != nil {
						sub = queueName(*q)
					}
				}
				if d.Description != "" {
					sub = d.Description
				}
				ui.Text(c, sub).FontSize(11).TextColor(muted).SingleLine()
			})
		})
	case colSize:
		ui.Text(c, totalSizeText(d)).FontSize(12).TextColor(muted).TextAlign(ui.End).SingleLine()
	case colProgress:
		a.progressCell(c, t, d, selected)
	case colSpeed:
		ui.Text(c, engine.FormatSpeed(d.Speed)).FontSize(12).TextColor(muted).TextAlign(ui.End)
	case colStatus:
		ui.Text(c, statusText(d)).FontSize(12).TextColor(status).SingleLine()
	case colAdded:
		ui.Text(c, addedText(d.Added)).FontSize(12).TextColor(muted).TextAlign(ui.End)
	}
}

func (a *App) progressCell(c *ui.Context, t *ui.Theme, d *model.Download, selected bool) {
	p := d.Percent()
	text := t.TextMuted
	if selected {
		text = t.AccentText.Alpha(0.85)
	}
	if d.Status == model.StatusComplete {
		col := t.Success
		if selected {
			col = t.AccentText
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, brand.Check).FontSize(13).TextColor(col)
			ui.Text(c, statusText(d)).FontSize(12).TextColor(col)
		})
		return
	}
	if d.Status == model.StatusError {
		col := t.Danger
		if selected {
			col = t.AccentText
		}
		ui.Text(c, a.errorText(d)).FontSize(11).TextColor(col).SingleLine()
		return
	}
	track := t.Border
	fill := t.Accent
	if selected {
		track = t.AccentText.Alpha(0.25)
		fill = t.AccentText
	}
	if p < 0 {
		ui.Box(c).Width(96).Height(6).Radius(99).Background(track).Children(func() {
			ui.Box(c).WidthPercent(38).Height(6).Radius(99).Background(fill)
		})
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(96).Height(6).Radius(99).Background(track).Children(func() {
			if p > 0 {
				ui.Box(c).WidthPercent(float32(p * 100)).Height(6).Radius(99).Background(fill)
			}
		})
		ui.Text(c, fmt.Sprintf("%d%%", int(p*100))).FontSize(11).TextColor(text)
	})
}

func (a *App) detail(c *ui.Context, t *ui.Theme, d *model.Download) {
	ui.Scroll(c).Fill().Background(t.Surface).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Column(c).Padding(16, 18).Gap(12).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, kindIcon(d)).FontSize(18).TextColor(t.Accent)
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, d.FileName).FontSize(15).Bold().SingleLine()
					ui.Text(c, d.URL).FontSize(11).TextColor(t.TextMuted).SingleLine()
				})
				a.iconButton(c, t, brand.Link, locales.DetailCopyLink(), func() { a.CopyURL(d) })
				a.iconButton(c, t, brand.Folder, locales.DetailShowInFolder(), func() { a.ShowInFolder(d) })
			})

			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Grow(1).Children(func() {
					ui.Progress(c, maxf(d.Percent(), 0)).Height(8)
				})
				ui.Text(c, percentText(d)).FontSize(12).TextColor(t.TextMuted).Width(46).TextAlign(ui.End)
			})

			ui.Row(c).Gap(24).Wrap().Children(func() {
				a.stat(c, t, locales.DetailSize(), sizeText(d))
				a.stat(c, t, locales.DetailDownloaded(), engine.FormatBytes(d.Done))
				a.stat(c, t, locales.DetailSpeed(), engine.FormatSpeed(d.Speed))
				a.stat(c, t, locales.DetailTimeLeft(), etaText(d.ETA()))
				a.stat(c, t, locales.DetailConnections(), fmt.Sprint(max(d.Connections, 1)))
				a.stat(c, t, locales.DetailStatus(), statusText(d))
			})

			if d.Error != "" && d.Status != model.StatusError {
				ui.Text(c, a.errorText(d)).FontSize(11).TextColor(t.Warning)
			}

			if len(d.Segments) > 1 {
				ui.Column(c).Gap(5).Children(func() {
					ui.Text(c, locales.DetailConnections()).FontSize(11).TextColor(t.TextMuted)
					ui.Row(c).Gap(2).Height(10).Children(func() {
						for i, s := range d.Segments {
							if i >= 80 {
								break
							}
							col := t.Border
							if s.Remaining() == 0 {
								col = t.Accent
							} else if s.Done > 0 {
								col = t.Warning
							}
							ui.Box(c).Grow(1).Height(10).Radius(3).Background(col)
						}
					})
				})
			}

			ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
				if d.Status.Active() {
					if ui.Button(c, locales.Pause()).Clicked() {
						a.Pause(d.ID)
					}
				} else if d.Status == model.StatusComplete {
					if ui.PrimaryButton(c, locales.DetailOpen()).Clicked() {
						a.OpenFile(d)
					}
				} else {
					if ui.PrimaryButton(c, locales.Resume()).Clicked() {
						a.Resume(d.ID)
					}
				}
				if ui.Button(c, locales.Remove()).Clicked() {
					a.askRemove(d, false)
				}
				ui.Spacer(c)
				ui.Text(c, d.Dir).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
		})
	})
}

func (a *App) stat(c *ui.Context, t *ui.Theme, name, value string) {
	ui.Column(c).Gap(1).Children(func() {
		ui.Text(c, name).FontSize(10).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(12)
	})
}

func (a *App) statusBar(c *ui.Context, t *ui.Theme, w int) {
	ui.Row(c).Height(30).Padding(0, 14).Gap(14).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Background(t.Surface).Children(func() {
		ui.Icon(c, brand.Bolt).FontSize(12).TextColor(t.Accent)
		ui.Text(c, engine.FormatSpeed(a.TotalSpeed())).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, locales.StatusActive(a.ActiveCount())).FontSize(11).TextColor(t.TextMuted)
		if a.st.Settings.LimitEnabled && w >= 700 {
			ui.Text(c, locales.StatusLimit(engine.FormatBytes(a.st.Settings.LimitBytes), a.st.Settings.LimitHours)).
				FontSize(11).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		if a.st.Settings.RemoteEnabled && w >= 620 {
			ui.Text(c, locales.StatusRemote(strconv.Itoa(a.st.Settings.RemotePort))).FontSize(11).TextColor(t.TextMuted)
		}
		ui.Text(c, locales.StatusVersion(Version)).FontSize(11).TextColor(t.TextMuted)
	})
}

// --- Dialogs ---

func (a *App) dialogs(c *ui.Context, t *ui.Theme, w, h int) {
	a.addDialog(c, t, w)
	a.optionsDialog(c, t, w, h)
	a.confirmDialog(c, t, w)
	a.aboutDialog(c, t, w)
}

// dialogWidth keeps a dialog inside the window.
func dialogWidth(w, want, floor int) float32 {
	if w-32 < want {
		want = w - 32
	}
	if want < floor {
		want = floor
	}
	return float32(want)
}

func (a *App) addDialog(c *ui.Context, t *ui.Theme, w int) {
	ui.Modal(c, &a.addOpen, func() {
		f := &a.add
		ui.Column(c).Width(dialogWidth(w, 580, 320)).Padding(20).Gap(14).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.AddTitle()).FontSize(18).Bold()
				ui.Spacer(c)
				a.iconButton(c, t, brand.Close, locales.AboutClose(), func() { a.addOpen = false })
			})

			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, locales.AddLink()).FontSize(11).TextColor(t.TextMuted)
				ui.Row(c).Gap(8).Wrap().Children(func() {
					field := ui.TextInput(c, &f.URL).Placeholder("https://example.com/file.zip").AutoFocus().Grow(1).MinWidth(180)
					if field.Submitted() {
						a.probeAdd()
					}
					if ui.Button(c, locales.AddPaste()).Clicked() {
						f.URL = strings.TrimSpace(mygo.Clipboard.ReadText())
						a.probeAdd()
					}
					if ui.Button(c, locales.AddLookUp()).Clicked() {
						a.probeAdd()
					}
				})
				switch {
				case f.Probing:
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Width(12).Height(12)
						ui.Text(c, locales.AddProbing()).FontSize(11).TextColor(t.TextMuted)
					})
				case f.Err != "":
					ui.Text(c, f.Err).FontSize(11).TextColor(t.Danger)
				case f.Info != nil:
					ui.Row(c).Gap(8).Wrap().Children(func() {
						ui.Icon(c, brand.Check).FontSize(12).TextColor(t.Success)
						ui.Text(c, fmt.Sprintf("%s · %s", engine.FormatBytes(f.Info.Size), resumableText(f.Info.Resumable))).
							FontSize(11).TextColor(t.TextMuted)
						if f.Duplicate {
							ui.Text(c, "· "+locales.AddAlreadyInList()).FontSize(11).TextColor(t.Warning)
						}
					})
				}
			})

			ui.Row(c).Gap(12).Wrap().Children(func() {
				ui.Column(c).Gap(6).Grow(1).Basis(220).Children(func() {
					ui.Text(c, locales.AddSaveAs()).FontSize(11).TextColor(t.TextMuted)
					ui.TextInput(c, &f.FileName).Placeholder(locales.AddFileName())
				})
				ui.Column(c).Gap(6).Width(180).Children(func() {
					ui.Text(c, locales.AddCategory()).FontSize(11).TextColor(t.TextMuted)
					names := make([]string, 0, len(a.st.Categories))
					for _, cat := range a.st.Categories {
						names = append(names, categoryName(cat))
					}
					current := names[0]
					if cat := a.category(f.Category); cat != nil {
						current = categoryName(*cat)
					}
					ui.Select(c, &current, names).Changed()
					if cat := a.categoryByName(current); cat != nil && cat.ID != f.Category {
						f.Category = cat.ID
						if cat.Folder != "" {
							f.Dir = cat.Folder
						}
					}
				})
			})

			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, locales.AddSaveTo()).FontSize(11).TextColor(t.TextMuted)
				ui.Row(c).Gap(8).Children(func() {
					ui.TextInput(c, &f.Dir).Grow(1)
					if ui.Button(c, locales.AddBrowse()).Clicked() {
						a.browseFolder(&f.Dir)
					}
				})
			})

			conn := float64(f.Connections)
			ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, locales.AddConnections()).FontSize(11).TextColor(t.TextMuted)
				ui.NumberInput(c, &conn, 1, 32, 1).Width(90)
				f.Connections = int(conn)
				ui.Text(c, locales.AddConnectionsHint()).
					FontSize(11).TextColor(t.TextMuted)
			})

			open := f.Advanced
			ui.Collapsible(c, locales.AddAdvanced(), &open, func() {
				ui.Column(c).Gap(10).Children(func() {
					ui.TextInput(c, &f.Referer).Placeholder(locales.AddRefererHint()).Label(locales.AddReferer())
					ui.TextInput(c, &f.Cookies).Placeholder(locales.AddCookiesHint()).Label(locales.AddCookies())
					ui.TextInput(c, &f.UserAgent).Placeholder("User-Agent").Label(locales.AddUserAgent())
					ui.TextInput(c, &f.Checksum).Placeholder(locales.AddChecksumHint()).Label(locales.AddChecksum())
					ui.TextInput(c, &f.Description).Placeholder(locales.AddDescriptionHint()).Label(locales.AddDescription())
				})
			})
			f.Advanced = open

			ui.Row(c).Gap(8).Wrap().Justify(ui.End).Children(func() {
				if ui.Button(c, locales.AddCancel()).Clicked() {
					a.addOpen = false
				}
				if ui.Button(c, locales.AddLater()).Clicked() {
					f.StartNow = false
					f.Queue = "main"
					a.commitAdd()
				}
				if ui.PrimaryButton(c, locales.AddStart()).Clicked() {
					f.StartNow = true
					a.commitAdd()
				}
			})
		})
	})
}

func (a *App) optionsDialog(c *ui.Context, t *ui.Theme, w, h int) {
	ui.Modal(c, &a.optionsOpen, func() {
		dw := dialogWidth(w, 680, 340)
		dh := float32(560)
		if h-80 < 560 {
			dh = float32(h - 80)
		}
		if dh < 320 {
			dh = 320
		}
		ui.Column(c).Width(dw).Height(dh).Padding(20).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.OptionsTitle()).FontSize(18).Bold()
				ui.Spacer(c)
				a.iconButton(c, t, brand.Close, locales.AboutClose(), func() { a.optionsOpen = false })
			})
			ui.Tabs(c, &a.optionsTab, locales.TabGeneral(), locales.TabConnection(),
				locales.TabCategories(), locales.TabQueues(), locales.TabAdvanced())
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(14).Padding(0, 2).Children(func() {
					switch a.optionsTab {
					case 0:
						a.optionsGeneral(c, t)
					case 1:
						a.optionsConnection(c, t)
					case 2:
						a.optionsCategories(c, t)
					case 3:
						a.optionsQueues(c, t)
					case 4:
						a.optionsAdvanced(c, t)
					}
				})
			})
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, locales.OptionsDone()).Clicked() {
					a.optionsOpen = false
					a.applySettings()
					a.Save()
				}
			})
		})
	})
}

func (a *App) optionsGeneral(c *ui.Context, t *ui.Theme) {
	s := &a.st.Settings
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, locales.OptionsDownloadFolder()).FontSize(11).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c, &s.DownloadDir).Grow(1)
			if ui.Button(c, locales.AddBrowse()).Clicked() {
				a.browseFolder(&s.DownloadDir)
			}
		})
	})
	ui.Row(c).Gap(12).Wrap().Children(func() {
		ui.Column(c).Gap(6).Grow(1).Basis(150).Children(func() {
			ui.Text(c, locales.OptionsAppearance()).FontSize(11).TextColor(t.TextMuted)
			labels := themeLabels()
			current := labels[labelIndex(themeKeys(), s.Theme)]
			ui.Select(c, &current, labels).Changed()
			s.Theme = keyAt(themeKeys(), labelIndex(labels, current))
		})
		ui.Column(c).Gap(6).Grow(1).Basis(150).Children(func() {
			ui.Text(c, locales.OptionsLanguage()).FontSize(11).TextColor(t.TextMuted)
			keys := languageKeys()
			labels := a.languageLabels()
			current := labels[labelIndex(keys, s.Language)]
			if ui.Select(c, &current, labels).Changed() {
				a.SetLanguage(keyAt(keys, labelIndex(labels, current)))
			}
		})
		ui.Column(c).Gap(6).Width(120).Children(func() {
			ui.Text(c, locales.OptionsAccent()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c, &s.Accent).Placeholder("#3B6EF6")
		})
	})
	ui.Checkbox(c, &s.StartDownloads, locales.OptionsStartDownloads())
	ui.Checkbox(c, &s.ShowAddDialog, locales.OptionsShowAddDialog())
	ui.Checkbox(c, &s.ClipboardWatch, locales.OptionsClipboardWatch())
	ui.Checkbox(c, &s.MinimizeToTray, locales.OptionsMinimizeToTray())
	ui.Checkbox(c, &s.CloseToTray, locales.OptionsCloseToTray())
	ui.Checkbox(c, &s.ConfirmDelete, locales.OptionsConfirmDelete())
	ui.Checkbox(c, &s.NotifyOnComplete, locales.OptionsNotifyComplete())
	ui.Checkbox(c, &s.NotifyOnError, locales.OptionsNotifyError())
	ui.Checkbox(c, &s.FileDateFromServer, locales.OptionsFileDate())
}

// languageLabels names the languages the app shows, each in itself.
func (a *App) languageLabels() []string {
	keys := languageKeys()
	out := make([]string, len(keys))
	for i, tag := range keys {
		if tag == "" {
			out[i] = locales.LanguageSystem()
			continue
		}
		out[i] = autonym(tag)
	}
	return out
}

func (a *App) optionsConnection(c *ui.Context, t *ui.Theme) {
	s := &a.st.Settings
	conn := float64(s.Connections)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.OptionsConnectionsPerDownload()).Grow(1)
		ui.NumberInput(c, &conn, 1, 32, 1).Width(100)
		s.Connections = int(conn)
	})
	per := float64(s.PerDownloadLimit / 1024)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.OptionsLimitEach()).Grow(1)
		ui.NumberInput(c, &per, 0, 1024*1024, 64).Width(120)
		s.PerDownloadLimit = int64(per) * 1024
	})
	global := float64(s.GlobalLimit / 1024)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.OptionsLimitAll()).Grow(1)
		ui.NumberInput(c, &global, 0, 1024*1024, 64).Width(120)
		s.GlobalLimit = int64(global) * 1024
	})
	retries := float64(s.MaxRetries)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.OptionsRetries()).Grow(1)
		ui.NumberInput(c, &retries, 0, 50, 1).Width(100)
		s.MaxRetries = int(retries)
	})
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, locales.OptionsProxy()).FontSize(11).TextColor(t.TextMuted)
		ui.TextInput(c, &s.Proxy).Placeholder(locales.OptionsProxyHint())
	})
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, locales.OptionsBind()).FontSize(11).TextColor(t.TextMuted)
		ui.TextInput(c, &s.BindAdapter).Placeholder(locales.OptionsBindHint())
	})
	ui.Checkbox(c, &s.LimitEnabled, locales.OptionsLimitEnabled())
	if s.LimitEnabled {
		mb := float64(s.LimitBytes / (1 << 20))
		hours := float64(s.LimitHours)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, locales.OptionsNoMoreThan()).FontSize(12)
			ui.NumberInput(c, &mb, 1, 1024*1024, 50).Width(110)
			ui.Text(c, locales.OptionsMB()).FontSize(12)
			ui.NumberInput(c, &hours, 1, 720, 1).Width(80)
			ui.Text(c, locales.OptionsHours()).FontSize(12)
		})
		s.LimitBytes = int64(mb) << 20
		s.LimitHours = int(hours)
	}
}

func (a *App) optionsCategories(c *ui.Context, t *ui.Theme) {
	ui.Text(c, locales.OptionsCategoriesHint()).FontSize(12).TextColor(t.TextMuted)
	for i := range a.st.Categories {
		cat := &a.st.Categories[i]
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, categoryIcon(*cat)).FontSize(15).TextColor(t.TextMuted)
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, categoryName(*cat)).FontSize(13)
				ui.Text(c, strings.Join(cat.Types, " ")).FontSize(10).TextColor(t.TextMuted).SingleLine()
			})
			ui.Text(c, cat.Folder).FontSize(10).TextColor(t.TextMuted).SingleLine().MaxWidth(200)
			if !cat.Protected {
				a.iconButton(c, t, brand.Trash, locales.OptionsDeleteCategory(), func() { a.RemoveCategory(cat.ID) })
			}
		})
	}
	ui.Divider(c)
	ui.Row(c).Gap(8).AlignItems(ui.End).Wrap().Children(func() {
		ui.Column(c).Gap(4).Grow(1).Basis(160).Children(func() {
			ui.Text(c, locales.OptionsNewCategory()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c, &a.newCatName).Placeholder(locales.OptionsName())
		})
		ui.Column(c).Gap(4).Width(140).Children(func() {
			ui.Text(c, locales.OptionsFileTypes()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c, &a.newCatTypes).Placeholder(locales.OptionsTypesHint())
		})
		ui.Column(c).Gap(4).Width(180).Children(func() {
			ui.Text(c, locales.OptionsFolder()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c, &a.newCatFolder).Placeholder(locales.OptionsFolderHint())
		})
		if ui.Button(c, locales.OptionsAdd()).Clicked() {
			types := strings.Fields(a.newCatTypes)
			a.AddCategory(a.newCatName, a.newCatFolder, types)
			a.newCatName, a.newCatTypes, a.newCatFolder = "", "", ""
		}
	})
}

func (a *App) optionsQueues(c *ui.Context, t *ui.Theme) {
	for i := range a.st.Queues {
		q := &a.st.Queues[i]
		ui.Column(c).Gap(8).Padding(10).Radius(10).Background(t.Surface).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, brand.Queue).FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, queueName(*q)).FontSize(13).Bold().Grow(1)
				if a.queueRunning[q.ID] {
					if ui.Button(c, locales.OptionsStop()).Clicked() {
						a.StopQueue(q.ID)
					}
				} else {
					if ui.Button(c, locales.OptionsStart()).Clicked() {
						a.StartQueue(q.ID)
					}
				}
				if !q.Builtin {
					a.iconButton(c, t, brand.Trash, locales.OptionsDeleteQueue(), func() { a.RemoveQueue(q.ID) })
				}
			})
			par := float64(q.Parallel)
			ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, locales.OptionsQueueDownload()).FontSize(11).TextColor(t.TextMuted)
				ui.NumberInput(c, &par, 1, 32, 1).Width(80)
				ui.Text(c, locales.OptionsQueueAtOnce()).FontSize(11).TextColor(t.TextMuted)
				q.Parallel = int(par)
			})
			ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, locales.OptionsStartAt()).FontSize(11).TextColor(t.TextMuted)
				ui.TextInput(c, &q.StartAt).Placeholder("23:00").Width(80)
				ui.Text(c, locales.OptionsStopAt()).FontSize(11).TextColor(t.TextMuted)
				ui.TextInput(c, &q.StopAt).Placeholder("07:00").Width(80)
				ui.Text(c, locales.OptionsOnce()).FontSize(11).TextColor(t.TextMuted)
				ui.TextInput(c, &q.Once).Placeholder("2026-01-01").Width(110)
			})
			ui.Checkbox(c, &q.OnStartup, locales.OptionsOnStartup())
			ui.Checkbox(c, &q.Wake, locales.OptionsWake())
			ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, locales.OptionsWhenDone()).FontSize(11).TextColor(t.TextMuted)
				power := q.PowerWhenDone
				ui.Select(c, &power, a.powerLabels()).Width(130)
				q.PowerWhenDone = power
				ui.Checkbox(c, &q.ExitWhenDone, locales.OptionsQuit())
			})
		})
	}
	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		ui.Column(c).Gap(4).Grow(1).Children(func() {
			ui.Text(c, locales.OptionsNewQueue()).FontSize(11).TextColor(t.TextMuted)
			ui.TextInput(c, &a.newQueueName).Placeholder(locales.OptionsName())
		})
		if ui.Button(c, locales.OptionsAdd()).Clicked() {
			a.AddQueue(a.newQueueName)
			a.newQueueName = ""
		}
	})
}

// powerKeys are what a queue does when it finishes, in the order the
// Options dialog lists them.
func powerKeys() []string { return []string{"", "sleep", "shutdown", "hibernate"} }

func (a *App) powerLabels() []string {
	return []string{
		locales.OptionsPowerNever(), locales.OptionsPowerSleep(),
		locales.OptionsPowerShutdown(), locales.OptionsPowerHibernate(),
	}
}

func (a *App) optionsAdvanced(c *ui.Context, t *ui.Theme) {
	s := &a.st.Settings
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, locales.OptionsUserAgent()).FontSize(11).TextColor(t.TextMuted)
		ui.TextInput(c, &s.UserAgent)
	})
	ui.Checkbox(c, &s.RemoteEnabled, locales.OptionsRemote())
	port := float64(s.RemotePort)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.OptionsPort()).Grow(1)
		ui.NumberInput(c, &port, 1024, 65535, 1).Width(110)
		s.RemotePort = int(port)
	})
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, locales.OptionsToken()).FontSize(11).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			ui.TextInput(c, &s.APIToken).Grow(1).ReadOnly(true)
			if ui.Button(c, locales.OptionsCopy()).Clicked() {
				mygo.Clipboard.WriteText(s.APIToken)
				a.toast = locales.ToastTokenCopied()
			}
		})
	})
	ui.Divider(c)
	ui.Column(c).Gap(3).Children(func() {
		ui.Text(c, locales.OptionsDataFolder()).FontSize(11).TextColor(t.TextMuted)
		if a.store != nil {
			ui.Text(c, a.store.Dir()).FontSize(12).Selectable()
		}
		ui.Text(c, "downmygo "+Version).FontSize(11).TextColor(t.TextMuted)
	})
}

func (a *App) confirmDialog(c *ui.Context, t *ui.Theme, w int) {
	if a.confirm == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		q := a.confirm
		ui.Column(c).Width(dialogWidth(w, 420, 300)).Padding(20).Gap(12).Children(func() {
			ui.Text(c, q.Title).FontSize(16).Bold()
			ui.Text(c, q.Message).FontSize(13).TextColor(t.TextMuted)
			if q.Detail != "" {
				ui.Text(c, q.Detail).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.ConfirmCancel()).Clicked() {
					a.confirm = nil
				}
				label := locales.Remove()
				if q.Delete {
					label = locales.ConfirmDeleteFile()
				}
				if ui.PrimaryButton(c, label).Clicked() {
					action := q.Action
					a.confirm = nil
					if action != nil {
						action()
					}
				}
			})
		})
	})
	if !open {
		a.confirm = nil
	}
}

func (a *App) aboutDialog(c *ui.Context, t *ui.Theme, w int) {
	ui.Modal(c, &a.aboutOpen, func() {
		ui.Column(c).Width(dialogWidth(w, 380, 300)).Padding(24).Gap(12).Center().Children(func() {
			a.mark(c, 56, 18, 28)
			ui.Text(c, "downmygo").FontSize(20).Bold()
			ui.Text(c, locales.AppTagline()).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, locales.AboutVersion(Version)).FontSize(11).TextColor(t.TextMuted)
			if ui.Button(c, locales.AboutClose()).Clicked() {
				a.aboutOpen = false
			}
		})
	})
}

// --- Row menu ---

func (a *App) rowMenu(m *ui.Menu, d *model.Download) {
	if d.Status.Active() {
		if m.Item(locales.Pause()).Chosen() {
			a.Pause(d.ID)
		}
	} else if d.Status == model.StatusComplete {
		if m.Item(locales.MenuOpen()).Chosen() {
			a.OpenFile(d)
		}
	} else {
		if m.Item(locales.Resume()).Chosen() {
			a.Resume(d.ID)
		}
	}
	if m.Item(locales.MenuShowInFolder()).Chosen() {
		a.ShowInFolder(d)
	}
	if m.Item(locales.MenuCopyLink()).Chosen() {
		a.CopyURL(d)
	}
	m.Separator()
	m.Submenu(locales.MenuAddToQueue(), func(sm *ui.Menu) {
		if sm.Item(locales.MenuNoQueue()).Chosen() {
			a.SetDownloadQueue(d.ID, "")
		}
		for _, q := range a.st.Queues {
			if sm.Item(queueName(q)).Checked(d.Queue == q.ID).Chosen() {
				a.SetDownloadQueue(d.ID, q.ID)
			}
		}
	})
	m.Separator()
	if m.Item(locales.MenuRemoveFromList()).Chosen() {
		a.askRemove(d, false)
	}
	if m.Item(locales.MenuDeleteFile()).Danger().Chosen() {
		a.askRemove(d, true)
	}
}

func (a *App) askRemove(d *model.Download, deleteFile bool) {
	if !a.st.Settings.ConfirmDelete {
		a.Remove(d.ID, deleteFile)
		return
	}
	title := locales.ConfirmRemoveTitle()
	if deleteFile {
		title = locales.ConfirmDeleteTitle()
	}
	a.confirm = &Confirm{
		Title:   title,
		Message: d.FileName,
		Detail:  locales.ConfirmRemoveBody(),
		Delete:  deleteFile,
		Action:  func() { a.Remove(d.ID, deleteFile) },
	}
}

// --- helpers ---

func (a *App) browseFolder(dst *string) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    a.win,
			Title:     locales.BrowseFolderTitle(),
			Directory: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() { *dst = paths[0] })
	}()
}

func (a *App) applySettings() {
	a.global.SetRate(a.st.Settings.GlobalLimit)
	a.eng.SetLimits(a.st.Settings.PerDownloadLimit)
	if a.st.Settings.RemoteEnabled {
		_ = a.StartAPI(true)
	} else if a.api != nil {
		a.api.Close()
		a.api = nil
	}
}

func (a *App) categoryByName(name string) *model.Category {
	for i := range a.st.Categories {
		if categoryName(a.st.Categories[i]) == name {
			return &a.st.Categories[i]
		}
	}
	return nil
}

func (a *App) filterTitle() string {
	switch {
	case a.filter == "all" || a.filter == "":
		return locales.SidebarAll()
	case a.filter == "unfinished":
		return locales.SidebarUnfinished()
	case a.filter == "finished":
		return locales.SidebarFinished()
	case strings.HasPrefix(a.filter, "cat:"):
		if c := a.category(strings.TrimPrefix(a.filter, "cat:")); c != nil {
			return categoryName(*c)
		}
	case strings.HasPrefix(a.filter, "queue:"):
		if q := a.queue(strings.TrimPrefix(a.filter, "queue:")); q != nil {
			return queueName(*q)
		}
	}
	return locales.FilterDownloads()
}

func (a *App) statusColor(t *ui.Theme, d *model.Download) ui.Color {
	switch d.Status {
	case model.StatusComplete:
		return t.Success
	case model.StatusError:
		return t.Danger
	case model.StatusPaused:
		return t.TextMuted
	case model.StatusWaiting:
		return t.Warning
	case model.StatusConnecting, model.StatusDownloading:
		return t.Accent
	}
	return t.TextMuted
}

func statusText(d *model.Download) string {
	switch d.Status {
	case model.StatusQueued:
		return locales.StatusQueued()
	case model.StatusConnecting:
		return locales.StatusConnecting()
	case model.StatusDownloading:
		return locales.StatusDownloading()
	case model.StatusPaused:
		return locales.StatusPaused()
	case model.StatusComplete:
		return locales.StatusComplete()
	case model.StatusError:
		return locales.StatusError()
	case model.StatusWaiting:
		return locales.StatusWaiting()
	}
	return ""
}

func kindIcon(d *model.Download) *ui.SVG {
	switch d.Kind {
	case model.KindTorrent:
		return brand.Link
	case model.KindVideo:
		return brand.Play
	}
	return brand.File
}

func totalSizeText(d *model.Download) string {
	if d.Size <= 0 {
		return "—"
	}
	return engine.FormatBytes(d.Size)
}

func sizeText(d *model.Download) string {
	if d.Size <= 0 {
		return "—"
	}
	if d.Status == model.StatusComplete {
		return engine.FormatBytes(d.Size)
	}
	return engine.FormatBytes(d.Done) + " / " + engine.FormatBytes(d.Size)
}

func percentText(d *model.Download) string {
	if d.Status == model.StatusComplete {
		return "100%"
	}
	p := d.Percent()
	if p < 0 {
		return "—"
	}
	return fmt.Sprintf("%d%%", int(p*100))
}

func addedText(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	now := time.Now()
	loc := i18n.Locale()
	switch {
	case now.Sub(t) < time.Minute:
		return locales.TimeJustNow()
	case now.Sub(t) < time.Hour:
		return locales.TimeMinutesAgo(int(now.Sub(t).Minutes()))
	case now.Sub(t) < 24*time.Hour:
		return i18n.FormatDate(loc, t, i18n.DateOptions{Skeleton: "jm"})
	case now.Year() == t.Year():
		return i18n.FormatDate(loc, t, i18n.DateOptions{Skeleton: "MMMd"})
	}
	return i18n.FormatDate(loc, t, i18n.DateOptions{Skeleton: "yMMMd"})
}

// etaText says the time a download has left in the app's language.
func etaText(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return locales.EtaHoursMinutes(h, m)
	case m > 0:
		return locales.EtaMinutesSeconds(m, s)
	}
	return locales.EtaSeconds(s)
}

func resumableText(ok bool) string {
	if ok {
		return locales.AddResumable()
	}
	return locales.AddOneStream()
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// FileIconFor is used by the window icon and tests.
func FileIconFor(name string) *ui.SVG {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".rar", ".7z", ".tar", ".gz":
		return brand.Queue
	case ".mp3", ".flac", ".wav":
		return brand.Bolt
	case ".mp4", ".mkv", ".webm":
		return brand.Play
	}
	return brand.File
}
