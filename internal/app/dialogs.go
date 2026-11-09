package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	brand "github.com/downmygo/downmygo/internal/ui"
	"github.com/downmygo/downmygo/locales"
)

// extraDialogs draws the dialogs the newer features use: the link
// grabber, the subscriptions, and the update notice.
func (a *App) extraDialogs(c *ui.Context, t *ui.Theme, w int) {
	a.grabDialog(c, t, w)
	a.feedsDialog(c, t, w)
	a.updateDialog(c, t, w)
}

func (a *App) grabDialog(c *ui.Context, t *ui.Theme, w int) {
	ui.Modal(c, &a.grabOpen, func() {
		ui.Column(c).Width(dialogWidth(w, 620, 320)).Height(520).Padding(20).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.GrabTitle()).FontSize(18).Bold()
				ui.Spacer(c)
				a.iconButton(c, t, brand.Close, locales.AboutClose(), func() { a.grabOpen = false })
			})
			ui.Row(c).Gap(8).Children(func() {
				field := ui.TextInput(c, &a.grabPage).Placeholder(locales.GrabPlaceholder()).Grow(1)
				if field.Submitted() {
					a.Grab(a.grabPage)
				}
				if ui.Button(c, locales.GrabLook()).Clicked() {
					a.Grab(a.grabPage)
				}
			})
			switch {
			case a.grabBusy:
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Width(12).Height(12)
					ui.Text(c, locales.GrabBusy()).FontSize(11).TextColor(t.TextMuted)
				})
			case a.grabErr != "":
				ui.Text(c, a.grabErr).FontSize(11).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.GrabCount(len(a.grabLinks))).FontSize(12).TextColor(t.TextMuted)
				ui.Spacer(c)
				if ui.Button(c, locales.GrabAll()).Clicked() {
					for _, l := range a.grabLinks {
						a.grabPicked[l.URL] = true
					}
				}
				if ui.Button(c, locales.GrabNone()).Clicked() {
					for _, l := range a.grabLinks {
						a.grabPicked[l.URL] = false
					}
				}
			})
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					if len(a.grabLinks) == 0 && !a.grabBusy && a.grabErr == "" {
						ui.Text(c, locales.GrabEmpty()).FontSize(12).TextColor(t.TextMuted)
					}
					for _, l := range a.grabLinks {
						picked := a.grabPicked[l.URL]
						if ui.Checkbox(c, &picked, l.Name).Changed() {
							a.grabPicked[l.URL] = picked
						}
						ui.Text(c, l.URL).FontSize(10).TextColor(t.TextMuted).SingleLine()
					}
				})
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.AddCancel()).Clicked() {
					a.grabOpen = false
				}
				if ui.PrimaryButton(c, locales.GrabAdd()).Clicked() {
					added := 0
					for _, l := range a.grabLinks {
						if a.grabPicked[l.URL] && a.AddURL(l.URL) != nil {
							added++
						}
					}
					a.grabOpen = false
					a.toast = locales.ToastImported(added)
				}
			})
		})
	})
}

func (a *App) feedsDialog(c *ui.Context, t *ui.Theme, w int) {
	ui.Modal(c, &a.feedsOpen, func() {
		ui.Column(c).Width(dialogWidth(w, 640, 320)).Height(540).Padding(20).Gap(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locales.FeedsTitle()).FontSize(18).Bold()
				ui.Spacer(c)
				a.iconButton(c, t, brand.Close, locales.AboutClose(), func() { a.feedsOpen = false })
			})
			ui.Text(c, locales.FeedsHint()).FontSize(12).TextColor(t.TextMuted)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(8).Children(func() {
					if len(a.st.Subscriptions) == 0 {
						ui.Text(c, locales.FeedsEmpty()).FontSize(12).TextColor(t.TextMuted)
					}
					for i := range a.st.Subscriptions {
						sub := &a.st.Subscriptions[i]
						ui.Column(c).Gap(4).Padding(10).Radius(10).Background(t.Surface).Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								on := sub.Enabled
								if ui.Checkbox(c, &on, "").Changed() {
									sub.Enabled = on
									a.markDirty()
								}
								name := sub.Name
								if name == "" {
									name = sub.URL
								}
								ui.Text(c, name).FontSize(13).Bold().Grow(1).SingleLine()
								a.iconButton(c, t, brand.Trash, locales.FeedsDelete(), func() {
									a.RemoveSubscription(sub.ID)
								})
							})
							ui.Text(c, sub.URL).FontSize(10).TextColor(t.TextMuted).SingleLine()
							ui.Row(c).Gap(10).Wrap().Children(func() {
								ui.Text(c, strings.Join(sub.Extensions, " ")).FontSize(10).TextColor(t.TextMuted)
								ui.Text(c, fmt.Sprintf("%s %d %s", locales.FeedsEvery(), sub.EveryMin, locales.FeedsMinutes())).
									FontSize(10).TextColor(t.TextMuted)
								if !sub.LastCheck.IsZero() {
									ui.Text(c, addedText(sub.LastCheck)).FontSize(10).TextColor(t.TextMuted)
								}
								if sub.Error != "" {
									ui.Text(c, sub.Error).FontSize(10).TextColor(t.Danger).SingleLine()
								}
							})
						})
					}
				})
			})
			ui.Divider(c)
			ui.Row(c).Gap(8).AlignItems(ui.End).Wrap().Children(func() {
				ui.Column(c).Gap(4).Grow(1).Basis(200).Children(func() {
					ui.Text(c, locales.FeedsURL()).FontSize(11).TextColor(t.TextMuted)
					ui.TextInput(c, &a.newFeedURL).Placeholder("https://example.com/feed.xml")
				})
				ui.Column(c).Gap(4).Width(120).Children(func() {
					ui.Text(c, locales.FeedsTypes()).FontSize(11).TextColor(t.TextMuted)
					ui.TextInput(c, &a.newFeedTypes).Placeholder("mp4 mp3")
				})
				ui.Column(c).Gap(4).Width(110).Children(func() {
					ui.Text(c, locales.FeedsEvery()).FontSize(11).TextColor(t.TextMuted)
					minutes := float64(a.newFeedEvery)
					if minutes < 1 {
						minutes = 60
					}
					ui.NumberInput(c, &minutes, 5, 24*60, 5).Width(110)
					a.newFeedEvery = int(minutes)
				})
				if ui.Button(c, locales.FeedsAdd()).Clicked() {
					a.AddSubscription(a.newFeedURL, a.newFeedTypes, a.newFeedEvery)
					a.newFeedURL, a.newFeedTypes, a.newFeedEvery = "", "", 60
				}
			})
		})
	})
}

func (a *App) updateDialog(c *ui.Context, t *ui.Theme, w int) {
	if a.updateRel == nil {
		return
	}
	ui.Modal(c, &a.updateOpen, func() {
		rel := a.updateRel
		ui.Column(c).Width(dialogWidth(w, 420, 300)).Padding(24).Gap(12).Children(func() {
			ui.Text(c, locales.UpdateTitle()).FontSize(17).Bold()
			ui.Text(c, locales.UpdateBody(rel.Version, Version)).FontSize(13).TextColor(t.TextMuted)
			if rel.Notes != "" {
				ui.Text(c, firstLines(rel.Notes, 6)).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.UpdateLater()).Clicked() {
					a.updateOpen = false
				}
				if ui.PrimaryButton(c, locales.UpdateOpen()).Clicked() {
					a.OpenUpdate()
				}
			})
		})
	})
}

// firstLines keeps the first n lines of a note, for a dialog.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// AddSubscription adds a feed to watch.
func (a *App) AddSubscription(rawURL, types string, everyMin int) {
	rawURL = strings.TrimSpace(rawURL)
	if !looksLikeURL(rawURL) {
		a.toast = locales.ErrOnlyHTTP()
		return
	}
	if everyMin < 5 {
		everyMin = 60
	}
	a.st.Subscriptions = append(a.st.Subscriptions, storeSubscription(rawURL, types, everyMin, a.st.Settings.DownloadDir))
	a.markDirty()
	a.Save()
	a.checkFeeds()
}

// RemoveSubscription stops watching a feed.
func (a *App) RemoveSubscription(id string) {
	for i := range a.st.Subscriptions {
		if a.st.Subscriptions[i].ID == id {
			a.st.Subscriptions = append(a.st.Subscriptions[:i], a.st.Subscriptions[i+1:]...)
			a.markDirty()
			a.Save()
			return
		}
	}
}

// ImportDialog opens the file picker for a list of links.
func (a *App) ImportDialog() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win,
			Title:  locales.ImportTitle(),
		})
		if err != nil || len(paths) == 0 {
			return
		}
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() { a.ImportFile(paths[0]) })
	}()
}

// GrabDialog opens the link grabber.
func (a *App) GrabDialog() {
	a.grabOpen = true
	a.grabErr = ""
}
