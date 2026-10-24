// A copy of what the GBA's status screen shows, for "Save screenshot".
//
// The GBA only draws what it's told (CMD_STATUS, see status.go and b2s/gba/status.c). Every
// command that reaches the GBA is also run through gbaScreen.apply, which is a port of the GBA's
// status_command: the same 240x160 picture, drawn the same way (5x8 font in 6x9 cells, region
// map, pictures, background). Nothing is read back from the GBA.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
)

const (
	scW, scH   = 240, 160
	scCellW    = 6
	scCellH    = 9
	scTop      = 3 // 17 rows x 9 px = 153 px
	scFontH    = 8
	scMapMax   = 28 * 15
	scShotZoom = 4 // saved pictures are this many times bigger (the GBA's dots stay square and sharp)
)

func bgr(r, g, b int) uint16 { return uint16(r | g<<5 | b<<10) }

var (
	scBG   = bgr(2, 3, 6)
	scText = [8]uint16{bgr(31, 31, 31), bgr(16, 16, 18), bgr(8, 28, 10), bgr(31, 8, 8), bgr(31, 26, 6), bgr(10, 24, 31),
		bgr(31, 31, 31), bgr(31, 31, 31)}
	scMapCol  = [5]uint16{scBG, bgr(5, 13, 6), bgr(10, 22, 31), bgr(12, 10, 17), bgr(31, 18, 4)}
	scOnPhoto = [8]uint16{bgr(3, 5, 12), bgr(9, 10, 14), bgr(1, 13, 4), bgr(21, 2, 2), bgr(25, 9, 0), bgr(2, 8, 22),
		bgr(3, 5, 12), bgr(3, 5, 12)}
)

type scMark struct {
	x, y int
	on   bool
}

type scSprite struct {
	on, over   bool
	x, y, w, h int
}

// gbaScreen: the GBA's status screen (its video memory and the state status.c keeps)
type gbaScreen struct {
	mu     sync.Mutex
	jp     bool // the GBA draws with the Japanese font (kana)
	active bool // a status screen is up (else the GBA shows its own screen, which isn't copied)
	vram   [scW * scH]uint16
	back   [scW * scH]uint16 // the kept background picture (ST_OP_IMAGE8)
	backOK bool
	ch, co [stRows][stCols]byte

	mapW, mapH, mapX0, mapY0 int
	mapCells                 [scMapMax]byte
	mk                       [2]scMark // 0: you, 1: the cursor
	spr                      [stSprites]scSprite
	bkX0, bkY0, bkX1, bkY1   int // where the background is on the screen now (bkX1 = 0: nowhere)
}

func (g *gbaScreen) setJapanese(jp bool) { g.mu.Lock(); g.jp = jp; g.mu.Unlock() }

func word16(d []uint32, i int) uint16 { return uint16(d[i/2] >> (16 * (i & 1))) }
func word8(d []uint32, i int) byte    { return byte(d[i/4] >> (8 * (i & 3))) }

// apply runs one CMD_STATUS packet (args = [op, nwords, ...], as sent) the way the GBA does.
// A packet the GBA would refuse changes nothing here either.
func (g *gbaScreen) apply(args []uint32, data []uint32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(args) < 2 {
		return
	}
	switch int(args[0]) {
	case stOpClear:
		g.clearAll()
		g.active = true

	case stOpRows:
		if !g.active {
			g.clearAll()
			g.active = true
		}
		for k := 0; k+stRowWords <= len(data); k += stRowWords {
			row := int(data[k])
			if row >= stRows {
				return
			}
			for c := 0; c < stCols; c++ {
				ch := byte(data[k+1+c/4] >> (8 * (c & 3)))
				co := byte(data[k+11+c/8]>>(4*(c&7))) & 7
				g.put(row, c, ch, co)
			}
		}

	case stOpMap:
		if len(args) < 6 {
			return
		}
		w, h, x0, y0 := int(args[2]), int(args[3]), int(args[4]), int(args[5])
		if w*h > scMapMax || (w*h+3)/4 > len(data) || w == 0 || h == 0 ||
			x0+w*mapCell > scW || y0+h*mapCell > scH {
			return
		}
		if !g.active {
			g.clearAll()
			g.active = true
		}
		g.mapW, g.mapH, g.mapX0, g.mapY0 = w, h, x0, y0
		for i := 0; i < w*h; i++ {
			g.mapCells[i] = word8(data, i)
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				g.drawCell(x, y)
			}
		}
		g.drawMarks()

	case stOpMark:
		if len(args) < 5 {
			return
		}
		w := 0
		if len(args) >= 6 && args[5] == 1 {
			w = 1
		}
		ox, oy := g.mk[w].x, g.mk[w].y
		g.mk[w].x, g.mk[w].y = -1, -1
		if int(args[2]) < g.mapW {
			g.mk[w].x = int(args[2])
		}
		if int(args[3]) < g.mapH {
			g.mk[w].y = int(args[3])
		}
		g.mk[w].on = args[4] != 0
		if ox >= 0 {
			g.redrawMarkArea(ox, oy)
		}
		if g.mk[w].x >= 0 {
			g.redrawMarkArea(g.mk[w].x, g.mk[w].y)
		}
		g.drawMarks()

	case stOpSprite:
		g.sprite(args, data)

	case stOpImage:
		if len(args) < 8 {
			return
		}
		w, h, bx, by, bw, bh := int(args[2]), int(args[3]), int(args[4]), int(args[5]), int(args[6]), int(args[7])
		if w == 0 || h == 0 || w > scW || h > scH || len(data) < (w*h+1)/2 || bx+bw > scW || by+bh > scH {
			return
		}
		if !g.active {
			g.clearAll()
			g.active = true
		}
		g.mapW, g.mapH = 0, 0 // the region map is gone
		g.bkX1 = 0
		g.mk[0].x, g.mk[0].y, g.mk[1].x, g.mk[1].y = -1, -1, -1, -1
		g.fill(bx, by, bw, bh, scBG)
		// the biggest size of the same shape that fits: dw/w = dh/h
		dw, dh := bw, bh
		if w*bh > h*bw {
			dh = h * bw / w
		} else {
			dw = w * bh / h
		}
		if dw == 0 {
			dw = 1
		}
		if dh == 0 {
			dh = 1
		}
		x0, y0 := bx+(bw-dw)/2, by+(bh-dh)/2
		for y := 0; y < dh; y++ {
			sy := y * h / dh
			for x := 0; x < dw; x++ {
				g.vram[(y0+y)*scW+x0+x] = word16(data, sy*w+x*w/dw) & 0x7FFF
			}
		}

	case stOpImage8:
		if len(args) < 7 {
			return
		}
		x0, y0, w, h, nc := int(args[2]), int(args[3]), int(args[4]), int(args[5]), int(args[6])
		if w == 0 || h == 0 || x0+w > scW || y0+h > scH || nc == 0 || nc > 256 ||
			len(data) < (nc+1)/2+(w*h+3)/4 {
			return
		}
		if !g.active {
			g.clearAll()
			g.active = true
		}
		px := data[(nc+1)/2:]
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := int(word8(px, y*w+x))
				c := scBG
				if i < nc {
					c = word16(data, i) & 0x7FFF
				}
				g.back[(y0+y)*scW+x0+x] = c
				g.vram[(y0+y)*scW+x0+x] = c
			}
		}
		g.backOK = true
		g.mapW, g.mapH = 0, 0
		g.backShown(x0, y0, w, h)

	case stOpRestore:
		if len(args) < 6 || !g.backOK {
			return
		}
		x0, y0, w, h := int(args[2]), int(args[3]), int(args[4]), int(args[5])
		if x0+w > scW || y0+h > scH {
			return
		}
		if !g.active {
			g.clearAll()
			g.active = true
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				g.vram[(y0+y)*scW+x0+x] = g.back[(y0+y)*scW+x0+x]
			}
		}
		g.mapW, g.mapH = 0, 0
		g.backShown(x0, y0, w, h)

	case stOpLeave:
		g.leaveLocked()
	}
}

// leave: the GBA put its own screen back (any command but a status one does that, and so does
// ST_OP_LEAVE): there's no status screen to copy now.
func (g *gbaScreen) leave() { g.mu.Lock(); g.leaveLocked(); g.mu.Unlock() }

func (g *gbaScreen) leaveLocked() {
	if g.active { // status_leave: blank, then the GBA draws its own screen
		g.active = false
		for i := range g.vram {
			g.vram[i] = scBG
		}
	}
}

func (g *gbaScreen) fill(x, y, w, h int, c uint16) {
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			g.vram[(y+j)*scW+x+i] = c
		}
	}
}

func (g *gbaScreen) clearAll() {
	for i := range g.vram {
		g.vram[i] = scBG
	}
	for r := range g.ch {
		for c := range g.ch[r] {
			g.ch[r][c], g.co[r][c] = ' ', 0
		}
	}
	g.mapW, g.mapH = 0, 0
	g.mk[0].x, g.mk[0].y, g.mk[1].x, g.mk[1].y = -1, -1, -1, -1
	for i := range g.spr {
		g.spr[i].on = false
	}
	g.bkX1 = 0
}

// onBack: is a text cell on the background picture? then its text is drawn over it
func (g *gbaScreen) onBack(row, col int) bool {
	x, y := col*scCellW, scTop+row*scCellH
	return g.backOK && x >= g.bkX0 && x+scCellW <= g.bkX1 && y >= g.bkY0 && y+scCellH <= g.bkY1
}

// backShown: the background now covers this part of the screen: its text cells get drawn again
func (g *gbaScreen) backShown(x0, y0, w, h int) {
	if g.bkX1 == 0 {
		g.bkX0, g.bkY0, g.bkX1, g.bkY1 = x0, y0, x0+w, y0+h
	} else { // with what's already shown: the box around both
		g.bkX0, g.bkY0 = min(g.bkX0, x0), min(g.bkY0, y0)
		g.bkX1, g.bkY1 = max(g.bkX1, x0+w), max(g.bkY1, y0+h)
	}
	for r := 0; r < stRows; r++ {
		for c := 0; c < stCols; c++ {
			if g.onBack(r, c) {
				g.ch[r][c] = 0
			}
		}
	}
}

// underSprite: does a text cell's glyph (not its spacing line) touch a picture?
func (g *gbaScreen) underSprite(row, col int) bool {
	x, y := col*scCellW, scTop+row*scCellH+1
	for i := range g.spr {
		s := g.spr[i]
		if s.on && x < s.x+s.w && x+scCellW > s.x && y < s.y+s.h && y+scFontH > s.y {
			return true
		}
	}
	return false
}

// glyphBit: is pixel (x, y) of a text cell's 6x8 glyph set? ASCII and Latin-1 from the 5x8 font;
// with the Japanese font a kana is 12 px wide over two cells (its code, then 0xFF), drawn 2 px in.
func (g *gbaScreen) glyphBit(row, col, x, y int) bool {
	ch := g.ch[row][col]
	if g.jp && kanaOf[ch] != 0 {
		kx := x - 2
		return kx >= 0 && kx < 8 && kana8[kanaOf[ch]-1][y]>>kx&1 != 0
	}
	if ch == 0xFF {
		var l byte
		if col > 0 {
			l = g.ch[row][col-1]
		}
		if !(g.jp && kanaOf[l] != 0) {
			return false
		}
		kx := x + scCellW - 2
		return kx < 8 && kana8[kanaOf[l]-1][y]>>kx&1 != 0
	}
	if x < 1 { // 1 px in from the left, so the screen edge doesn't cut it
		return false
	}
	var i int
	switch {
	case ch >= 0x20 && ch < 0x7F:
		i = int(ch) - 0x20
	case ch >= 0xA0:
		i = int(ch) - 0xA0 + 95
	default:
		i = '?' - 0x20
	}
	return font5x8[i][y]>>(x-1)&1 != 0
}

func (g *gbaScreen) put(row, col int, ch, co byte) {
	if g.ch[row][col] == ch && g.co[row][col] == co {
		return
	}
	g.ch[row][col], g.co[row][col] = ch, co
	if col+1 < stCols && g.ch[row][col+1] == 0xFF {
		g.ch[row][col+1] = 0 // its right half: draw it again
	}
	if !g.underSprite(row, col) {
		g.drawGlyph(row, col)
	}
}

func (g *gbaScreen) drawGlyph(row, col int) {
	co := g.co[row][col] & 7
	x0, y0 := col*scCellW, scTop+row*scCellH
	if g.onBack(row, col) { // over the picture: its pixels, then the text in darker colours
		for y := 0; y < scCellH; y++ {
			for x := 0; x < scCellW; x++ {
				g.vram[(y0+y)*scW+x0+x] = g.back[(y0+y)*scW+x0+x]
			}
		}
		for y := 0; y < scFontH; y++ {
			for x := 0; x < scCellW; x++ {
				if g.glyphBit(row, col, x, y) {
					g.vram[(y0+y+1)*scW+x0+x] = scOnPhoto[co]
				}
			}
		}
		return
	}
	fg := scText[co]
	for x := 0; x < scCellW; x++ { // 1 px of space above
		g.vram[y0*scW+x0+x] = scBG
	}
	for y := 0; y < scFontH; y++ {
		for x := 0; x < scCellW; x++ {
			c := scBG
			if g.glyphBit(row, col, x, y) {
				c = fg
			}
			g.vram[(y0+1+y)*scW+x0+x] = c
		}
	}
}

// drawCell: one map cell: a filled square with a 1 px gap, so the shape of the region shows
func (g *gbaScreen) drawCell(x, y int) {
	t := int(g.mapCells[y*g.mapW+x])
	px, py := g.mapX0+x*mapCell, g.mapY0+y*mapCell
	g.fill(px, py, mapCell, mapCell, scBG)
	if t != 0 {
		g.fill(px, py, mapCell-1, mapCell-1, scMapCol[min(t, 4)])
	}
}

// drawMarks: the cursor as a yellow frame, you as white with a red centre
func (g *gbaScreen) drawMarks() {
	if g.mk[1].x >= 0 && g.mk[1].on {
		px, py, s := g.mapX0+g.mk[1].x*mapCell-2, g.mapY0+g.mk[1].y*mapCell-2, mapCell+3
		c := bgr(31, 28, 4)
		g.fill(px, py, s, 1, c)
		g.fill(px, py+s-1, s, 1, c)
		g.fill(px, py, 1, s, c)
		g.fill(px+s-1, py, 1, s, c)
	}
	if g.mk[0].x >= 0 && g.mk[0].on {
		px, py := g.mapX0+g.mk[0].x*mapCell, g.mapY0+g.mk[0].y*mapCell
		g.fill(px-1, py-1, mapCell+1, mapCell+1, bgr(31, 31, 31))
		g.fill(px+2, py+2, mapCell-5, mapCell-5, bgr(31, 6, 6))
	}
}

func (g *gbaScreen) redrawMarkArea(x, y int) {
	for j := y - 1; j <= y+1; j++ {
		for i := x - 1; i <= x+1; i++ {
			if i >= 0 && j >= 0 && i < g.mapW && j < g.mapH {
				g.drawCell(i, j)
			}
		}
	}
}

// sprite: ST_OP_SPRITE, args [op, n, slot, x, y, w, h, flags], data: 16 colours (8 words), then
// 4bpp tiles. With no data it takes the picture away.
func (g *gbaScreen) sprite(args []uint32, data []uint32) {
	if len(args) < 7 {
		return
	}
	slot, x, y, w, h := int(args[2]), int(args[3]), int(args[4]), int(args[5]), int(args[6])
	if slot >= stSprites {
		return
	}
	if !g.active {
		g.clearAll()
		g.active = true
	}
	s := &g.spr[slot]
	if s.on { // take the old picture away, put the text back
		ox, oy, ow, oh := s.x, s.y, s.w, s.h
		s.on = false
		if s.over && g.backOK { // over the kept background: put that back
			for j := 0; j < oh; j++ {
				for i := 0; i < ow; i++ {
					g.vram[(oy+j)*scW+ox+i] = g.back[(oy+j)*scW+ox+i]
				}
			}
		} else {
			g.fill(ox, oy, ow, oh, scBG)
			for r := 0; r < stRows; r++ {
				for c := 0; c < stCols; c++ {
					cx, cy := c*scCellW, scTop+r*scCellH
					if cx < ox+ow && cx+scCellW > ox && cy < oy+oh && cy+scCellH > oy && !g.underSprite(r, c) {
						g.drawGlyph(r, c)
					}
				}
			}
		}
	}
	if len(data) == 0 {
		return
	}
	if (w|h)&7 != 0 || w == 0 || h == 0 || x+w > scW || y+h > scH || len(data) < 8+w*h/8 {
		return
	}
	s.on, s.x, s.y, s.w, s.h = true, x, y, w, h
	s.over = len(args) >= 8 && args[7]&1 != 0 && g.backOK
	var pal [16]uint16
	for i := range pal {
		pal[i] = word16(data, i) & 0x7FFF
	}
	tiles := data[8:]
	tw := w / 8
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			tile := (py/8)*tw + px/8
			b := word8(tiles, tile*32+(py&7)*4+(px&7)/2)
			c := b & 15
			if px&1 != 0 {
				c = b >> 4
			}
			if c != 0 {
				g.vram[(y+py)*scW+x+px] = pal[c]
			} else if !s.over { // over the picture: what's there shows through
				g.vram[(y+py)*scW+x+px] = scBG
			}
		}
	}
}

// ---------------------------------------------------------------------------- the picture

// image: the screen as it is now (nil when no status screen is up)
func (g *gbaScreen) image(zoom int) *image.RGBA {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.active {
		return nil
	}
	if zoom < 1 {
		zoom = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, scW*zoom, scH*zoom))
	for y := 0; y < scH; y++ {
		for x := 0; x < scW; x++ {
			v := int(g.vram[y*scW+x] & 0x7FFF)
			r, gr, b := v&31, v>>5&31, v>>10&31
			c := color.RGBA{uint8(r<<3 | r>>2), uint8(gr<<3 | gr>>2), uint8(b<<3 | b>>2), 255}
			for j := 0; j < zoom; j++ {
				for i := 0; i < zoom; i++ {
					img.SetRGBA(x*zoom+i, y*zoom+j, c)
				}
			}
		}
	}
	return img
}

// savePNG writes the screen to path. It says why not if no status screen is up.
func (g *gbaScreen) savePNG(path string) error {
	img := g.image(scShotZoom)
	if img == nil {
		return fmt.Errorf("the GBA isn't showing a status screen")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
