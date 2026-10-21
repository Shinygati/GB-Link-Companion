/* Minimal text output in bitmap mode 3 (240x160, 8x12 cells = 30x13 characters). */
#include "gba.h"
#include "ui.h"
#include "font.h"
#include "font_kana.h"

u8 ui_jp;                                  /* the Japanese font: kana instead of 0x01-0x1F, 0x5C, 0x60, 0x7F-0xFE */

#define RGB(r, g, b) ((u16)((r) | (g) << 5 | (b) << 10))
static const u16 colors[] = { RGB(31,31,31), RGB(16,16,18), RGB(8,28,10), RGB(31,8,8), RGB(31,26,6), RGB(10,24,31) };
#define BG RGB(2,3,6)

void ui_init(void)
{
    REG_DISPCNT = 0x0403;                  /* mode 3, BG2 on */
    for (int i = 0; i < 240 * 160; i++) VRAM16[i] = BG;
}

static void put(int row, int col, int color, char ch)
{
    u8 c = (u8)ch;
    if (row < 0 || row >= 13 || col < 0 || col >= 30) return;
    vu16 *p = VRAM16 + (row * FONT_H + 2) * 240 + col * 8;
    if (ui_jp && kana_of[c]) {             /* a kana: 8x8, 2 px down in the cell */
        const unsigned char *k = kana8[kana_of[c] - 1];
        for (int y = 0; y < FONT_H; y++, p += 240) {
            int r = y - 2;
            for (int x = 0; x < 8; x++) p[x] = (r >= 0 && r < 8 && (k[r] >> x & 1)) ? colors[color] : BG;
        }
        return;
    }
    int i = c >= 0x20 && c < 0x7F ? c - 0x20 : c >= 0xA0 ? c - 0xA0 + 95 : '?' - 0x20;
    const unsigned char *g = font8x12[i];
    for (int y = 0; y < FONT_H; y++, p += 240)
        for (int x = 0; x < 8; x++) p[x] = (g[y] >> x & 1) ? colors[color] : BG;
}

void ui_clear_line(int row)
{
    for (int c = 0; c < 30; c++) put(row, c, 0, ' ');
}

void ui_text(int row, int col, int color, const char *s)
{
    while (*s) put(row, col++, color, *s++);
}

void ui_hex(int row, int col, int color, u32 v, int digits)
{
    for (int i = digits - 1; i >= 0; i--) put(row, col++, color, "0123456789ABCDEF"[(v >> (i * 4)) & 15]);
}

void ui_dec(int row, int col, int color, u32 v)
{
    char buf[11]; int n = 0;
    do { u32 q = 0, r = v; while (r >= 10) { r -= 10; q++; } buf[n++] = (char)('0' + r); v = q; } while (v);
    while (n) put(row, col++, color, buf[--n]);
}

#define BAR_W 140
void ui_bar(int row, u32 done, u32 total)
{
    u32 pct = 0;
    if (total) {                       /* done * 100 / total, kept within 32 bits */
        while (total > 40000000u) { total >>= 1; done >>= 1; }
        if (done > total) done = total;
        pct = done * 100 / total;
    }
    /* bar from x=56 to 195: the percentage starts at column 26 (x=208), so they never touch */
    int fill = (int)(pct * BAR_W / 100);
    vu16 *p = VRAM16 + (row * FONT_H + 2 + 2) * 240 + 56;
    for (int y = 0; y < 8; y++, p += 240)
        for (int x = 0; x < BAR_W; x++) p[x] = (y == 0 || y == 7 || x == 0 || x == BAR_W - 1) ? colors[C_GREY]
                                              : x < fill ? colors[C_GREEN] : BG;
    ui_text(row, 27, C_WHITE, "   ");
    ui_dec(row, pct >= 100 ? 26 : pct >= 10 ? 27 : 28, C_WHITE, pct);
    ui_text(row, 29, C_WHITE, "%");
}
