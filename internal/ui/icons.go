// Package ui holds downmygo's look: the brand palette and the icons the
// window draws. It has no app state, so the theme can be tested alone.
package ui

import (
	"github.com/egoist/mygo/ui"
)

func icon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

func solid(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor">` + body + `</svg>`))
}

// Mark is the app's own picture: a download arrow that lands on a
// progress bar, the same geometry as the icon tools/makeicon draws.
var Mark = solid(`<path d="M12 4.6v11.2" stroke="currentColor" stroke-width="3.1" stroke-linecap="round"/><path d="M7.4 11.9 12 16.5l4.6-4.6" stroke="currentColor" stroke-width="3.1" stroke-linecap="round" stroke-linejoin="round" fill="none"/><rect x="7" y="18.3" width="10" height="1.9" rx="0.95" opacity="0.42"/><rect x="7" y="18.3" width="6.2" height="1.9" rx="0.95"/>`)

var (
	Download  = icon(`<path d="M12 3v12"/><path d="M7.5 10.5 12 15l4.5-4.5"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/>`)
	Plus      = icon(`<path d="M12 5v14"/><path d="M5 12h14"/>`)
	Pause     = icon(`<path d="M8.5 5v14"/><path d="M15.5 5v14"/>`)
	Play      = solid(`<path d="M7 4.5 19.5 12 7 19.5z"/>`)
	Stop      = solid(`<rect x="6" y="6" width="12" height="12" rx="2"/>`)
	Trash     = icon(`<path d="M4 7h16"/><path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/><path d="M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13"/>`)
	Folder    = icon(`<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`)
	Settings  = icon(`<path d="M4 7h9"/><path d="M17 7h3"/><circle cx="15" cy="7" r="2"/><path d="M4 17h3"/><path d="M11 17h9"/><circle cx="9" cy="17" r="2"/>`)
	Search    = icon(`<circle cx="11" cy="11" r="7"/><path d="M16.5 16.5 21 21"/>`)
	Check     = icon(`<path d="M4.5 12.5 9.5 17.5 20 6.5"/>`)
	Alert     = icon(`<path d="M12 3.5 22 20H2z"/><path d="M12 9v5"/><path d="M12 17.2v.1"/>`)
	Clock     = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/>`)
	Link      = icon(`<path d="M10 13a5 5 0 0 0 7 0l2-2a5 5 0 0 0-7-7l-1 1"/><path d="M14 11a5 5 0 0 0-7 0l-2 2a5 5 0 0 0 7 7l1-1"/>`)
	Close     = icon(`<path d="M6 6l12 12"/><path d="M18 6 6 18"/>`)
	Copy      = icon(`<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3"/>`)
	Queue     = icon(`<path d="M4 6h16"/><path d="M4 12h16"/><path d="M4 18h10"/>`)
	Chevron   = icon(`<path d="M6 9l6 6 6-6"/>`)
	Bolt      = solid(`<path d="M13 2 4 14h6l-1 8 9-12h-6z"/>`)
	Globe     = icon(`<circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3c3 3.5 3 14.5 0 18"/><path d="M12 3c-3 3.5-3 14.5 0 18"/>`)
	File      = icon(`<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v4h4"/>`)
	Info      = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><path d="M12 8v.1"/>`)
	Refresh   = icon(`<path d="M20 11a8 8 0 1 0-2.3 6.3"/><path d="M20 4v7h-7"/>`)
	Sun       = icon(`<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`)
	Moon      = icon(`<path d="M20 14.5A8.5 8.5 0 0 1 9.5 4 8.5 8.5 0 1 0 20 14.5z"/>`)
	ArrowUp   = icon(`<path d="M12 19V5"/><path d="M6 11l6-6 6 6"/>`)
	ArrowDown = icon(`<path d="M12 5v14"/><path d="M6 13l6 6 6-6"/>`)
)
