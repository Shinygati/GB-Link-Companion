"""The GBA helper's fonts for every language the Pokemon games come in, and its translated text.

Writes:
  gba/font.h        8x12 font (the helper's own screen): ASCII + Latin-1, from DejaVu Sans Mono
  gba/font_small.h  5x8 font (the status screens): ASCII + Latin-1, from Spleen 5x8
  gba/font_kana.h   8x8 kana from Misaki Gothic 2nd (Num Kadoma, free licence), and which code is which
  gba/lang.h        the helper's text in English, French, German, Italian, Spanish and Japanese
Codes: ASCII 0x20-0x7E and Latin-1 0xA0-0xFF are themselves. With the Japanese font, the kana take
the codes 0x01-0x1F, 0x5C, 0x60 and 0x7F-0xFE instead (in the order of KANA in lang.json), and on
the status screens a kana is two cells wide: its code, then 0xFF.
Usage: python3 tools/mkfonts_lang.py path/to/spleen-5x8.bdf path/to/misaki_gothic_2nd.ttf path/to/lang.json"""
import json, re, sys
from PIL import Image, ImageDraw, ImageFont

spleen, misaki, langfile = sys.argv[1:4]
LATIN = list(range(0x20, 0x7F)) + list(range(0xA0, 0x100))      # 191 glyphs, in this order
L = json.load(open(langfile, encoding="utf-8"))
KANA = L["kana"]
SLOTS = list(range(0x01, 0x20)) + [0x5C, 0x60] + list(range(0x7F, 0xFF))
assert len(SLOTS) == len(KANA) == 161

def cname(c):
    return repr(chr(c)).replace("*/", "* /").encode("ascii", "backslashreplace").decode()

# ---- 8x12 (DejaVu Sans Mono 11 px): accented capitals are drawn a pixel higher to fit
F = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 11)
asc, desc = F.getmetrics()
big = []
for c in LATIN:
    im = Image.new("L", (8, 14), 0)
    ImageDraw.Draw(im).text((0, 12 - asc), chr(c), font=F, fill=255)
    rows = [sum(1 << x for x in range(8) if im.getpixel((x, y)) > 96) for y in range(14)]
    top = rows[:2]
    rows = rows[2:]
    if any(top):                          # taller than the cell (accented capitals): squeeze the top
        rows = [top[0] | top[1] | rows[0]] + rows[1:] if not any(rows[:1]) else [top[1] | rows[0]] + rows[1:]
    big.append(rows)
with open("gba/font.h", "w") as f:
    f.write("/* 8x12 font: ASCII 0x20-0x7E and Latin-1 0xA0-0xFF rendered from DejaVu Sans Mono\n"
            " * (Bitstream Vera / DejaVu fonts license - free to use and redistribute). Bit 0 = left pixel.\n"
            " * Index: code - 0x20 for ASCII, code - 0xA0 + 95 for Latin-1. Made by tools/mkfonts_lang.py. */\n"
            f"#define FONT_H 12\nstatic const unsigned char font8x12[{len(LATIN)}][12] = {{\n")
    for c, g in zip(LATIN, big):
        f.write("    {" + ",".join(f"0x{v:02X}" for v in g) + "}, /* " + cname(c) + " */\n")
    f.write("};\n")

# ---- 5x8 (Spleen)
glyphs, cur, bm = {}, None, None
for line in open(spleen):
    p = line.split()
    if not p: continue
    if p[0] == "ENCODING": cur = int(p[1])
    elif p[0] == "BITMAP": bm = []
    elif p[0] == "ENDCHAR": glyphs[cur] = bm; bm = None
    elif bm is not None: bm.append(int(p[0], 16))
with open("gba/font_small.h", "w") as f:
    f.write("/* 5x8 font for the status screens: ASCII 0x20-0x7E and Latin-1 0xA0-0xFF from Spleen 5x8.\n"
            " * Spleen: Copyright (c) 2018-2026, Frederic Cambus. BSD 2-Clause license (see LICENSE-Spleen).\n"
            " * Bit 0 = left pixel. Index as font.h. Made by tools/mkfonts_lang.py. */\n"
            f"#define SFONT_H 8\nstatic const unsigned char font5x8[{len(LATIN)}][8] = {{\n")
    for c in LATIN:
        g = glyphs.get(c, glyphs[ord("?")])
        rows = g + [0] * (8 - len(g))
        out = [sum(1 << x for x in range(8) if r >> (7 - x) & 1) for r in rows]
        f.write("    {" + ",".join(f"0x{v:02X}" for v in out) + "}, /* " + cname(c) + " */\n")
    f.write("};\n")

# ---- kana (Misaki Gothic 2nd, 8x8 bitmap font)
M = ImageFont.truetype(misaki, 8)
kana = []
for k in KANA:
    im = Image.new("L", (8, 8), 0)
    ImageDraw.Draw(im).text((0, 0), k, font=M, fill=255)
    kana.append([sum(1 << x for x in range(8) if im.getpixel((x, y)) > 127) for y in range(8)])
slot_of = [0] * 256
for i, s in enumerate(SLOTS):
    slot_of[s] = i + 1
with open("gba/font_kana.h", "w") as f:
    f.write("/* 8x8 kana for the Japanese games, from Misaki Gothic 2nd by Num Kadoma (https://littlelimit.net/misaki.htm):\n"
            " * \"Unlimited permission is granted to use, copy, and distribute it, with or without modification,\n"
            " * either commercially and noncommercially.\" Bit 0 = left pixel. Made by tools/mkfonts_lang.py. */\n"
            f"static const unsigned char kana8[{len(KANA)}][8] = {{\n")
    for k, g in zip(KANA, kana):
        f.write("    {" + ",".join(f"0x{v:02X}" for v in g) + "}, /* U+%04X */\n" % ord(k))
    f.write("};\n/* with the Japanese font: code -> kana8 index + 1 (0: not a kana) */\n"
            "static const unsigned char kana_of[256] = {\n")
    for i in range(0, 256, 16):
        f.write("    " + ",".join(f"{v:3d}" for v in slot_of[i:i + 16]) + ",\n")
    f.write("};\n")

# ---- the helper's text
LANGS = ["E"] + L["langs"]
def enc(s, jp):
    out = []
    for ch in s:
        o = ord(ch)
        if jp and ch in KANA: out.append(SLOTS[KANA.index(ch)])
        elif 0x20 <= o < 0x7F and not (jp and o in (0x5C, 0x60)): out.append(o)
        elif not jp and 0xA0 <= o <= 0xFF: out.append(o)
        else: raise SystemExit(f"can't show {ch!r} in {s!r}")
    return "".join(chr(b) if (0x20 <= b < 0x7F and chr(b) not in '"\\?') else "\\%03o" % b for b in out)
keys = list(L["gba"].keys())
def sname(k): return "S_" + re.sub("[^A-Z0-9]+", "_", k.upper()).strip("_")
with open("gba/lang.h", "w") as f:
    f.write("/* The helper's own text in the Pokemon games' languages (from gblink-companion-native/assets/lang.json).\n"
            " * Encoded for its fonts (font.h, font_kana.h). Made by tools/mkfonts_lang.py. */\n"
            f"#define N_LANGS {len(LANGS)}\nstatic const char lang_codes[] = \"{''.join(LANGS)}\";\n")
    f.write("enum {\n" + "".join(f"    {sname(k)},\n" for k in keys) + f"    N_STRINGS\n}};\n")
    f.write("static const char *const lang_text[N_LANGS][N_STRINGS] = {\n")
    for li, lang in enumerate(LANGS):
        f.write("    {\n")
        for k in keys:
            t = k if lang == "E" else L["gba"][k][li - 1]
            f.write(f"        \"{enc(t, lang == 'J')}\",\n")
        f.write("    },\n")
    f.write("};\n")
print("wrote gba/font.h, gba/font_small.h, gba/font_kana.h, gba/lang.h;", len(keys), "strings")
for k in keys:
    print(f"  {sname(k)}: {k}")
