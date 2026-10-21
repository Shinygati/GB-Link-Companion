"""Render printable ASCII from DejaVu Sans Mono into an 8x12 1bpp font table (gba/font.h)."""
import sys
from PIL import Image, ImageDraw, ImageFont
H = 12
F = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 11)
TH = 96
rows = []
asc, desc = F.getmetrics()
for c in range(32, 127):
    im = Image.new("L", (8, H), 0)
    ImageDraw.Draw(im).text((0, 10 - asc), chr(c), font=F, fill=255)
    rows.append([sum(1 << x for x in range(8) if im.getpixel((x, y)) > TH) for y in range(H)])
if "--show" in sys.argv:
    for c in sys.argv[-1]:
        g = rows[ord(c) - 32]
        print(c); [print("".join("#" if r >> x & 1 else "." for x in range(8))) for r in g]
else:
    with open("gba/font.h", "w") as f:
        f.write("/* 8x12 font: printable ASCII 0x20-0x7E rendered from DejaVu Sans Mono\n"
                " * (Bitstream Vera / DejaVu fonts license - free to use and redistribute). Bit 0 = left pixel. */\n"
                f"#define FONT_H {H}\nstatic const unsigned char font8x12[95][{H}] = {{\n")
        for c, g in zip(range(32, 127), rows):
            f.write("    {" + ",".join(f"0x{v:02X}" for v in g) + "}, /* " + repr(chr(c)).replace("*/", "* /") + " */\n")
        f.write("};\n")
