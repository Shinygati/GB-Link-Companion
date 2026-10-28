// The Cartridge tools page (back up, write a save, write a flash cart, for Game Boy and GBA
// cartridges) and the Pokémon Bank's choice between the GBA and Game Boy banks. Both open in
// place of the games list; Back or the top bar's Home closes them.
package main

import (
	"image"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"
)

var toolOrder = []struct{ id, title, text string }{
	{"gba-save-backup", "Back up a GBA save", "Copy only the cartridge's save to this PC: done in seconds, without copying the game."},
	{"gba-backup", "Back up a GBA cartridge", "Copy the game and its save to this PC with GB-Link Cart Doctor's fast dump, checked against No-Intro's list of good dumps."},
	{"gba-restore", "Write a save to a GBA cartridge", "Replace the cartridge's save with a .sav file. The cartridge's own save is copied to this PC first."},
	{"gba-flash", "Write a ROM to a GBA flash cart", "Put a .gba game on a flash or reproduction cart. Every cart type FlashGBX knows, read back to check it."},
	{"gb-save-backup", "Back up a Game Boy save", "Copy only a Game Boy or Game Boy Color cartridge's save to this PC, without copying the game."},
	{"gb-backup", "Back up a Game Boy cartridge", "Game Boy and Game Boy Color: copy the game and its save to this PC. The GBA becomes a Game Boy Color for this."},
	{"gb-restore", "Write a save to a Game Boy cartridge", "Replace a Game Boy or Game Boy Color cartridge's save with a .sav file. Its own save is copied first."},
	{"gb-flash", "Write a ROM to a Game Boy flash cart", "Put a .gb or .gbc game on a Game Boy flash cart (carts written through the cartridge's normal write pin)."},
}

func (w *Window) toolsClicks(gtx C) {
	a := w.app
	for _, id := range []string{"tools-open", "tools-open-2"} {
		if w.b(id).Clicked(gtx) {
			w.toolsPage = "tools"
			w.page.Position = layout.Position{}
		}
	}
	for _, id := range []string{"bank-top", "bank-home"} {
		if w.b(id).Clicked(gtx) {
			w.settingsOpen, w.setPage = false, ""
			w.toolsPage = "bank"
			w.page.Position = layout.Position{}
			if w.st.Phase != "home" {
				go a.homeButton()
			}
		}
	}
	if w.b("tools-back").Clicked(gtx) {
		w.toolsPage = ""
	}
	if w.b("tools-back-done").Clicked(gtx) {
		w.toolsPage = "tools"
		a.goHome()
	}
	run := func(tool string) {
		w.toolsPage = ""
		go func() {
			if err := a.startTool(tool); err != nil {
				a.ui.Toast(err.Error())
			}
		}()
	}
	for _, t := range toolOrder {
		if w.b("tool:" + t.id).Clicked(gtx) {
			run(t.id)
		}
	}
	if w.b("tool-again").Clicked(gtx) && w.st.Tool != "" {
		tool := w.st.Tool
		a.goHome()
		run(tool)
	}
	// the old buttons (kept for anything that still sends them): the GBA tools
	if w.b("backup").Clicked(gtx) || w.b("backup-2").Clicked(gtx) || w.b("backup-again").Clicked(gtx) {
		run("gba-backup")
	}
	if w.b("restore").Clicked(gtx) || w.b("restore-again").Clicked(gtx) {
		run("gba-restore")
	}
}

// toolCard: one tool, as a big button.
func (w *Window) toolCard(id, title, text string) layout.Widget {
	return func(gtx C) D {
		cl := w.b(id)
		return layout.Inset{Bottom: 10}.Layout(gtx, func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				border := w.pal.border
				if cl.Hovered() {
					border = mix(w.pal.accent, w.pal.border, .45)
				}
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return w.box(gtx, w.pal.surface, border, 12, 0, func(gtx C) D {
					return layout.Inset{Top: 14, Bottom: 14, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return w.label(gtx, 15.5, font.SemiBold, w.pal.text, title) }),
							layout.Rigid(gap(3)),
							layout.Rigid(func(gtx C) D { return w.label(gtx, 13, font.Normal, w.pal.muted, text) }))
					})
				})
			})
		})
	}
}

func (w *Window) toolsView() []layout.Widget {
	heading := func(t string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Top: 18, Bottom: 8, Left: 4}.Layout(gtx, func(gtx C) D {
				return w.label(gtx, 12.5, font.Bold, w.pal.muted, t)
			})
		}
	}
	head := func(title, sub string) layout.Widget {
		return func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return w.label(gtx, 24, font.SemiBold, w.pal.text, title) }),
						layout.Rigid(func(gtx C) D { return w.label(gtx, 13.5, font.Normal, w.pal.muted, sub) }))
				}),
				layout.Rigid(func(gtx C) D { return w.button(gtx, w.b("tools-back"), "secondary", "Back", false, nil) }))
		}
	}
	if w.toolsPage == "bank" {
		return []layout.Widget{
			head("Pokémon Bank", "Two banks, kept apart: Pokémon move between the games of one bank, never between the two."),
			heading("GAME BOY ADVANCE"),
			w.toolCard("bank-gba", "Pokémon Emerald, FireRed and LeafGreen", "Your GBA cartridge's boxes over GB-Link, beside the boxes kept on this PC."),
			heading("GAME BOY AND GAME BOY COLOR"),
			w.toolCard("gbbank", "Pokémon Red, Blue, Yellow, Gold, Silver and Crystal",
				"A cartridge's boxes over GB-Link (the GBA becomes a Game Boy Color), or a save file from a flash cart's SD card or an emulator. "+
					"Pokémon move between all six games, as with the Time Capsule."),
		}
	}
	blocks := []layout.Widget{head("Cartridge tools", "Back up your cartridges, write a save back to one, or write a game to a flash cart.")}
	blocks = append(blocks, heading("GAME BOY ADVANCE"))
	for _, t := range toolOrder[:4] {
		blocks = append(blocks, w.toolCard("tool:"+t.id, t.title, t.text))
	}
	blocks = append(blocks, heading("GAME BOY AND GAME BOY COLOR"))
	for _, t := range toolOrder[4:] {
		blocks = append(blocks, w.toolCard("tool:"+t.id, t.title, t.text))
	}
	blocks = append(blocks, func(gtx C) D {
		return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
			return w.label(gtx, 12.5, font.Normal, w.pal.muted, "Backups, Game Boy cartridges and flash-cart writing use GB-Link Cart Doctor's programs and FlashGBX's cart types (both GPL-3.0).")
		})
	})
	return blocks
}

// gbPlayNote: what the GBA does while a Game Boy game is played.
func (w *Window) gbPlayNote(gtx C) D {
	return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D {
		return w.box(gtx, w.pal.surface2, w.pal.border, 12, 14, func(gtx C) D {
			return w.label(gtx, 13.5, font.Normal, w.pal.text, "This is a Game Boy game, so the GBA is being a Game Boy Color: it can't be a controller or a second screen. "+
				"Leave it on with the cartridge in, and each save is copied to the cartridge and checked. If it's switched off, your save waits on this PC.")
		})
	})
}

var _ = image.Pt
var _ widget.Clickable
