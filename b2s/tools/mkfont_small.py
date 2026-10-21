"""Converts Spleen 5x8 (BSD-2-Clause, Frederic Cambus) into gba/font_small.h for the status screens.
Usage: python3 tools/mkfont_small.py path/to/spleen-5x8.bdf"""
import sys
glyphs, cur, bm, bbx = {}, None, None, None
for line in open(sys.argv[1]):
    p = line.split()
    if not p:
        continue
    if p[0] == 'ENCODING': cur = int(p[1])
    elif p[0] == 'BBX': bbx = list(map(int, p[1:]))
    elif p[0] == 'BITMAP': bm = []
    elif p[0] == 'ENDCHAR': glyphs[cur] = bm; bm = None
    elif bm is not None: bm.append(int(p[0], 16))
with open('gba/font_small.h', 'w') as f:
    f.write("/* 5x8 font for the status screens: printable ASCII 0x20-0x7E from Spleen 5x8.\n"
            " * Spleen: Copyright (c) 2018-2026, Frederic Cambus. BSD 2-Clause license (see LICENSE-Spleen).\n"
            " * Bit 0 = left pixel. */\n#define SFONT_H 8\nstatic const unsigned char font5x8[95][8] = {\n")
    for c in range(32, 127):
        rows = glyphs[c] + [0] * (8 - len(glyphs[c]))
        out = [sum(1 << x for x in range(8) if r >> (7 - x) & 1) for r in rows]
        f.write("    {" + ",".join(f"0x{v:02X}" for v in out) + "}, /* " + repr(chr(c)).replace("*/", "* /") + " */\n")
    f.write("};\n")
