package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"

	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/locales"
)

// The names of the things every install has are kept in the state in
// English, because they name folders and identify the category or queue;
// what the window says is the language's name for them. A category or a
// queue the user made keeps the name the user gave it.

func categoryName(cat model.Category) string {
	if !cat.Builtin {
		return cat.Name
	}
	switch cat.ID {
	case "general":
		return locales.CatGeneral()
	case "compressed":
		return locales.CatCompressed()
	case "documents":
		return locales.CatDocuments()
	case "music":
		return locales.CatMusic()
	case "video":
		return locales.CatVideo()
	case "programs":
		return locales.CatPrograms()
	case "torrents":
		return locales.CatTorrents()
	}
	return cat.Name
}

func queueName(q model.Queue) string {
	if !q.Builtin {
		return q.Name
	}
	switch q.ID {
	case "main":
		return locales.QueueMain()
	case "sync":
		return locales.QueueSync()
	}
	return q.Name
}

// themeKeys are the theme settings, in the order the Options dialog lists
// them; themeLabels say them in the app's language.
func themeKeys() []string { return []string{"system", "light", "dark"} }

func themeLabels() []string {
	return []string{locales.ThemeSystem(), locales.ThemeLight(), locales.ThemeDark()}
}

// languageKeys are the languages the app shows, "" following the system.
func languageKeys() []string { return []string{"", "en", "zh-Hans", "zh-Hant"} }

// labelIndex finds the index of key in keys, or 0.
func labelIndex(keys []string, key string) int {
	for i, k := range keys {
		if k == key {
			return i
		}
	}
	return 0
}

// keyAt returns keys[i], or keys[0] when i is out of range.
func keyAt(keys []string, i int) string {
	if i < 0 || i >= len(keys) {
		return keys[0]
	}
	return keys[i]
}

// autonym names a language in itself, as a menu of languages does:
// "English", "简体中文", "繁體中文".
func autonym(tag string) string { return i18n.Autonym(tag) }

// errorText says a download's error in the language the app shows.
func (a *App) errorText(d *model.Download) string {
	if d.Error == errLimit {
		return locales.ErrLimitReached()
	}
	return d.Error
}

// ApplyLanguage makes the app show the language the settings chose, ""
// following the user's own languages.
func (a *App) ApplyLanguage() {
	tag := a.st.Settings.Language
	if labelIndex(languageKeys(), tag) == 0 {
		tag = ""
	}
	mygo.App.SetLocale(tag)
}

// SetLanguage makes the app show the language of tag at once, and keeps
// it for the next launches.
func (a *App) SetLanguage(tag string) {
	if labelIndex(languageKeys(), tag) == 0 {
		tag = ""
	}
	a.st.Settings.Language = tag
	mygo.App.SetLocale(tag)
	a.markDirty()
	a.Save()
}
