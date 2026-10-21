/* Status screens drawn for the mGBA save-sync launcher: text grid + region map (mode 3). */
#include "gba.h"
#include "proto.h"
#include "status.h"
#include "font_small.h"

#ifdef B2S_SIM
static u16 sim_vram[240 * 160];          /* the simulator has no screen: draw into memory */
#undef VRAM16
#define VRAM16 sim_vram
#endif

#define RGB(r, g, b) ((u16)((r) | (g) << 5 | (b) << 10))
#define BG     RGB(2, 3, 6)
#define CELL_W 6
#define CELL_H 9
#define TOP    3                          /* 17 rows x 9 px = 153 px, centred-ish */

/* text colours (same order as ui.h), then the map's */
static const u16 tcol[8] = { RGB(31,31,31), RGB(16,16,18), RGB(8,28,10), RGB(31,8,8), RGB(31,26,6), RGB(10,24,31),
                             RGB(31,31,31), RGB(31,31,31) };
static const u16 mcol[5] = { BG, RGB(5,13,6), RGB(10,22,31), RGB(12,10,17), RGB(31,18,4) };

static int active;
static u8 shown_ch[ST_ROWS][ST_COLS], shown_co[ST_ROWS][ST_COLS];
static u8 map_w, map_h, map_x0, map_y0;
static u8 map_cells[ST_MAP_MAX];
static struct { int x, y, on; } mk[2] = {{-1, -1, 0}, {-1, -1, 0}};   /* 0: you, 1: the cursor */
static struct { u8 on, w, h, over; u16 x, y; } spr[ST_SPRITES];   /* pictures: text under them isn't drawn */
static u16 back[240 * 160];              /* the kept background picture (ST_OP_IMAGE8) */
static int back_ok;
static int bk_x0, bk_y0, bk_x1, bk_y1;   /* where it's on the screen now (x1 = 0: nowhere) */

/* is a text cell on the background picture? then its text is drawn over it, with a shadow */
static int on_back(int row, int col)
{
    int x = col * CELL_W, y = TOP + row * CELL_H;
    return back_ok && x >= bk_x0 && x + CELL_W <= bk_x1 && y >= bk_y0 && y + CELL_H <= bk_y1;
}

/* the picture now covers this part of the screen: its text cells get drawn again */
static void back_shown(int x0, int y0, int w, int h)
{
    if (bk_x1 == 0) { bk_x0 = x0; bk_y0 = y0; bk_x1 = x0 + w; bk_y1 = y0 + h; }
    else {                                 /* with what's already shown: the box around both */
        if (x0 < bk_x0) bk_x0 = x0;
        if (y0 < bk_y0) bk_y0 = y0;
        if (x0 + w > bk_x1) bk_x1 = x0 + w;
        if (y0 + h > bk_y1) bk_y1 = y0 + h;
    }
    for (int r = 0; r < ST_ROWS; r++)
        for (int c = 0; c < ST_COLS; c++)
            if (on_back(r, c)) shown_ch[r][c] = 0;
}

/* does a text cell's glyph (not its spacing line) touch a picture? */
static int under_sprite(int row, int col)
{
    int x = col * CELL_W, y = TOP + row * CELL_H + 1;
    for (int i = 0; i < ST_SPRITES; i++)
        if (spr[i].on && x < spr[i].x + spr[i].w && x + CELL_W > spr[i].x && y < spr[i].y + spr[i].h && y + SFONT_H > spr[i].y)
            return 1;
    return 0;
}

int status_active(void) { return active; }

static void fill(int x, int y, int w, int h, u16 c)
{
    for (int j = 0; j < h; j++) {
        vu16 *p = VRAM16 + (y + j) * 240 + x;
        for (int i = 0; i < w; i++) p[i] = c;
    }
}

static void clear_all(void)
{
    for (int i = 0; i < 240 * 160; i++) VRAM16[i] = BG;
    for (int r = 0; r < ST_ROWS; r++)
        for (int c = 0; c < ST_COLS; c++) { shown_ch[r][c] = ' '; shown_co[r][c] = 0; }
    map_w = map_h = 0;
    mk[0].x = mk[0].y = mk[1].x = mk[1].y = -1;
    for (int i = 0; i < ST_SPRITES; i++) spr[i].on = 0;
    bk_x1 = 0;
}

static void draw_glyph(int row, int col);
static void put(int row, int col, u8 ch, u8 co)
{
    if (ch < 32 || ch > 126) ch = '?';
    if (shown_ch[row][col] == ch && shown_co[row][col] == co) return;
    shown_ch[row][col] = ch; shown_co[row][col] = co;
    if (!under_sprite(row, col)) draw_glyph(row, col);
}

static void draw_glyph(int row, int col)
{
    u8 ch = shown_ch[row][col], co = shown_co[row][col];
    if (on_back(row, col)) {               /* over the picture: its pixels, then the text in darker colours */
        static const u16 dark[8] = { RGB(3,5,12), RGB(9,10,14), RGB(1,13,4), RGB(21,2,2), RGB(25,9,0), RGB(2,8,22), RGB(3,5,12), RGB(3,5,12) };
        int x0 = col * CELL_W, y0 = TOP + row * CELL_H;
        const unsigned char *g = font5x8[(ch < 32 || ch > 126 ? '?' : ch) - 32];
        for (int y = 0; y < CELL_H; y++)
            for (int x = 0; x < CELL_W; x++) VRAM16[(y0 + y) * 240 + x0 + x] = back[(y0 + y) * 240 + x0 + x];
        for (int y = 0; y < SFONT_H; y++)
            for (int x = 0; x < 5; x++)
                if (g[y] >> x & 1) VRAM16[(y0 + y + 1) * 240 + x0 + x + 1] = dark[co & 7];
        return;
    }
    const unsigned char *g = font5x8[ch - 32];
    u16 fg = tcol[co & 7];
    vu16 *p = VRAM16 + (TOP + row * CELL_H) * 240 + col * CELL_W;
    for (int x = 0; x < CELL_W; x++) p[x] = BG;                     /* 1 px of space above */
    p += 240;
    for (int y = 0; y < SFONT_H; y++, p += 240)
        for (int x = 0; x < CELL_W; x++) p[x] = (x >= 1 && (g[y] >> (x - 1) & 1)) ? fg : BG;   /* 1 px in from the left, so the screen edge doesn't cut it */
}

/* one map cell: a filled square with a 1 px gap, so the shape of the region shows */
static void draw_cell(int x, int y)
{
    u8 t = map_cells[y * map_w + x];
    int px = map_x0 + x * ST_MAP_CELL, py = map_y0 + y * ST_MAP_CELL;
    fill(px, py, ST_MAP_CELL, ST_MAP_CELL, BG);
    if (t) fill(px, py, ST_MAP_CELL - 1, ST_MAP_CELL - 1, mcol[t > 4 ? 4 : t]);
}

/* the markers on top: the cursor as a yellow frame, you as white with a red centre */
static void draw_marks(void)
{
    if (mk[1].x >= 0 && mk[1].on) {
        int px = map_x0 + mk[1].x * ST_MAP_CELL - 2, py = map_y0 + mk[1].y * ST_MAP_CELL - 2, s = ST_MAP_CELL + 3;
        u16 c = RGB(31, 28, 4);
        fill(px, py, s, 1, c); fill(px, py + s - 1, s, 1, c); fill(px, py, 1, s, c); fill(px + s - 1, py, 1, s, c);
    }
    if (mk[0].x >= 0 && mk[0].on) {
        int px = map_x0 + mk[0].x * ST_MAP_CELL, py = map_y0 + mk[0].y * ST_MAP_CELL;
        fill(px - 1, py - 1, ST_MAP_CELL + 1, ST_MAP_CELL + 1, RGB(31, 31, 31));
        fill(px + 2, py + 2, ST_MAP_CELL - 5, ST_MAP_CELL - 5, RGB(31, 6, 6));
    }
}

static void redraw_mark_area(int x, int y)
{
    for (int j = y - 1; j <= y + 1; j++)
        for (int i = x - 1; i <= x + 1; i++)
            if (i >= 0 && j >= 0 && i < map_w && j < map_h) draw_cell(i, j);
}

void status_leave(void)
{
    if (!active) return;
    active = 0;
    for (int i = 0; i < 240 * 160; i++) VRAM16[i] = BG;
}

u32 status_command(const u32 *args, u32 nargs, const u32 *data, u32 ndata)
{
    if (nargs < 2) return ST_BAD_COMMAND;
    switch (args[0]) {
    case ST_OP_CLEAR:
        clear_all();
        active = 1;
        return ST_OK;

    case ST_OP_ROWS:                     /* per row: [row], 10 words of chars, 5 words of colours */
        if (!active) { clear_all(); active = 1; }
        for (u32 k = 0; k + ST_ROW_WORDS <= ndata; k += ST_ROW_WORDS) {
            u32 row = data[k];
            if (row >= ST_ROWS) return ST_TOO_BIG;
            for (int c = 0; c < ST_COLS; c++) {
                u8 ch = (u8)(data[k + 1 + c / 4] >> (8 * (c & 3)));
                u8 co = (u8)(data[k + 11 + c / 8] >> (4 * (c & 7))) & 7;
                put(row, c, ch, co);
            }
        }
        return ST_OK;

    case ST_OP_MAP: {                    /* args [op, n, w, h, x0, y0], cells 4 per word */
        if (nargs < 6) return ST_BAD_COMMAND;
        u32 w = args[2], h = args[3];
        if (w * h > ST_MAP_MAX || (w * h + 3) / 4 > ndata || w == 0 || h == 0) return ST_TOO_BIG;
        if (args[4] + w * ST_MAP_CELL > 240 || args[5] + h * ST_MAP_CELL > 160) return ST_TOO_BIG;
        if (!active) { clear_all(); active = 1; }
        map_w = (u8)w; map_h = (u8)h; map_x0 = (u8)args[4]; map_y0 = (u8)args[5];
        for (u32 i = 0; i < w * h; i++) map_cells[i] = (u8)(data[i / 4] >> (8 * (i & 3)));
        for (u32 y = 0; y < h; y++)
            for (u32 x = 0; x < w; x++) draw_cell(x, y);
        draw_marks();
        return ST_OK;
    }

    case ST_OP_MARK: {                   /* args [op, 0, x, y, on, which]: you (0) or the cursor (1); x 255 = none */
        if (nargs < 5) return ST_BAD_COMMAND;
        int w = nargs >= 6 && args[5] == 1;
        int ox = mk[w].x, oy = mk[w].y;
        mk[w].x = args[2] < map_w ? (int)args[2] : -1;
        mk[w].y = args[3] < map_h ? (int)args[3] : -1;
        mk[w].on = args[4] != 0;
        if (ox >= 0) redraw_mark_area(ox, oy);
        if (mk[w].x >= 0) redraw_mark_area(mk[w].x, mk[w].y);
        draw_marks();
        return ST_OK;
    }

    case ST_OP_SPRITE: {
        if (nargs < 7) return ST_BAD_COMMAND;
        u32 slot = args[2], x = args[3], y = args[4], w = args[5], h = args[6];
        if (slot >= ST_SPRITES) return ST_TOO_BIG;
        if (!active) { clear_all(); active = 1; }
        if (spr[slot].on) {                    /* take the old picture away, put the text back */
            int ox = spr[slot].x, oy = spr[slot].y, ow = spr[slot].w, oh = spr[slot].h;
            spr[slot].on = 0;
            if (spr[slot].over && back_ok) {   /* over the kept background: put that back */
                for (int j = 0; j < oh; j++)
                    for (int i = 0; i < ow; i++) VRAM16[(oy + j) * 240 + ox + i] = back[(oy + j) * 240 + ox + i];
                if (ndata == 0) return ST_OK;
                goto draw;
            }
            fill(ox, oy, ow, oh, BG);
            for (int r = 0; r < ST_ROWS; r++)
                for (int c = 0; c < ST_COLS; c++) {
                    int cx = c * CELL_W, cy = TOP + r * CELL_H;
                    if (cx < ox + ow && cx + CELL_W > ox && cy < oy + oh && cy + CELL_H > oy && !under_sprite(r, c))
                        draw_glyph(r, c);
                }
        }
        if (ndata == 0) return ST_OK;
    draw:
        if ((w | h) & 7 || w == 0 || h == 0 || x + w > 240 || y + h > 160 || ndata < 8 + w * h / 8) return ST_TOO_BIG;
        spr[slot].on = 1; spr[slot].x = (u16)x; spr[slot].y = (u16)y; spr[slot].w = (u8)w; spr[slot].h = (u8)h;
        spr[slot].over = nargs >= 8 && (args[7] & 1) && back_ok;
        u16 pal[16];
        for (int i = 0; i < 16; i++) pal[i] = (u16)(data[i / 2] >> (16 * (i & 1))) & 0x7FFF;
        const u8 *t = (const u8 *)(data + 8);
        u32 tw = w / 8;
        for (u32 py = 0; py < h; py++) {
            vu16 *p = VRAM16 + (y + py) * 240 + x;
            for (u32 px = 0; px < w; px++) {
                u32 tile = (py / 8) * tw + px / 8;
                u8 b = t[tile * 32 + (py & 7) * 4 + (px & 7) / 2];
                u8 c = (px & 1) ? b >> 4 : b & 15;
                if (c) p[px] = pal[c];
                else if (!spr[slot].over) p[px] = BG;   /* over the picture: what's there shows through */
            }
        }
        return ST_OK;
    }

    case ST_OP_IMAGE: {                  /* a picture of a place, scaled to fit its box */
        if (nargs < 8) return ST_BAD_COMMAND;
        u32 w = args[2], h = args[3], bx = args[4], by = args[5], bw = args[6], bh = args[7];
        if (w == 0 || h == 0 || w > 240 || h > 160 || ndata < (w * h + 1) / 2 || bx + bw > 240 || by + bh > 160) return ST_TOO_BIG;
        if (!active) { clear_all(); active = 1; }
        map_w = map_h = 0;                     /* the region map is gone */
        bk_x1 = 0;
        mk[0].x = mk[0].y = mk[1].x = mk[1].y = -1;
        fill(bx, by, bw, bh, BG);
        /* the biggest size of the same shape that fits: dw/w = dh/h */
        u32 dw = bw, dh = bh;
        if (w * bh > h * bw) dh = h * bw / w; else dw = w * bh / h;
        if (dw == 0) dw = 1;
        if (dh == 0) dh = 1;
        u32 x0 = bx + (bw - dw) / 2, y0 = by + (bh - dh) / 2;
        static u16 colx[240];
        for (u32 x = 0; x < dw; x++) colx[x] = (u16)(x * w / dw);
        const u16 *c = (const u16 *)data;
        for (u32 y = 0; y < dh; y++) {
            const u16 *src = c + (y * h / dh) * w;
            vu16 *p = VRAM16 + (y0 + y) * 240 + x0;
            for (u32 x = 0; x < dw; x++) p[x] = src[colx[x]] & 0x7FFF;
        }
        return ST_OK;
    }

    case ST_OP_IMAGE8: {                 /* a 1:1 indexed picture, kept as the background */
        if (nargs < 7) return ST_BAD_COMMAND;
        u32 x0 = args[2], y0 = args[3], w = args[4], h = args[5], nc = args[6];
        if (w == 0 || h == 0 || x0 + w > 240 || y0 + h > 160 || nc == 0 || nc > 256 || ndata < (nc + 1) / 2 + (w * h + 3) / 4) return ST_TOO_BIG;
        if (!active) { clear_all(); active = 1; }
        const u16 *pal = (const u16 *)data;
        const u8 *px = (const u8 *)(data + (nc + 1) / 2);
        for (u32 y = 0; y < h; y++)
            for (u32 x = 0; x < w; x++) {
                u8 i = px[y * w + x];
                u16 c = i < nc ? pal[i] & 0x7FFF : BG;
                back[(y0 + y) * 240 + x0 + x] = c;
                VRAM16[(y0 + y) * 240 + x0 + x] = c;
            }
        back_ok = 1;
        map_w = map_h = 0;
        back_shown(x0, y0, w, h);
        return ST_OK;
    }

    case ST_OP_RESTORE: {                /* the kept background again */
        if (nargs < 6) return ST_BAD_COMMAND;
        u32 x0 = args[2], y0 = args[3], w = args[4], h = args[5];
        if (!back_ok) return ST_NO_SAVE;      /* nothing kept (the GBA restarted): send it again */
        if (x0 + w > 240 || y0 + h > 160) return ST_TOO_BIG;
        if (!active) { clear_all(); active = 1; }
        for (u32 y = 0; y < h; y++)
            for (u32 x = 0; x < w; x++) VRAM16[(y0 + y) * 240 + x0 + x] = back[(y0 + y) * 240 + x0 + x];
        map_w = map_h = 0;
        back_shown(x0, y0, w, h);
        return ST_OK;
    }

    case ST_OP_LEAVE:
        return ST_OK;                    /* the caller puts its own screen back */
    }
    return ST_BAD_COMMAND;
}
