"""GB-Link Companion app icon: a magenta gradient tile with a white outline glyph of a GBA-style
handheld (an original drawing, no logos). Writes icon<size>.png for each size into the given folder."""
from PIL import Image, ImageDraw, ImageFilter, ImageChops
import sys
S = 1024
TOP, BOT = (214, 36, 196), (140, 16, 170)
WHITE = (255, 255, 255)

def rr(box, r):
    m = Image.new("L", (S, S), 0); ImageDraw.Draw(m).rounded_rectangle(box, r, fill=255); return m
def over(im, col, mask, a=255):
    l = Image.new("RGBA", (S, S), col + (0,)); l.putalpha(mask.point(lambda v: v * a // 255))
    return Image.alpha_composite(im, l)

def draw(size):
    small = size <= 32
    im = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    pad, rad = (36, 230) if not small else (0, 200)
    tile = rr((pad, pad, S - pad, S - pad), rad)
    g = Image.new("RGBA", (S, S)); gd = ImageDraw.Draw(g)
    for y in range(S):
        t = y / (S - 1); gd.line((0, y, S, y), fill=tuple(int(TOP[i] * (1 - t) + BOT[i] * t) for i in range(3)) + (255,))
    im.paste(g, (0, 0), tile)
    if not small:   # soft light from the top
        m = Image.new("L", (S, S), 0); ImageDraw.Draw(m).ellipse((-250, -700, S + 250, 420), fill=40)
        im = over(im, WHITE, ImageChops.multiply(m.filter(ImageFilter.GaussianBlur(50)), tile))

    sw = 34                                   # outline stroke width
    x0, x1, y0, y1 = 150, S - 150, 330, 700
    # body outline (with the shoulder bumps at the top corners)
    body = rr((x0, y0, x1, y1), 140)
    for sx in (x0 + 20, x1 - 300):   # low, wide shoulder buttons
        body = ImageChops.lighter(body, rr((sx, y0 - 26, sx + 280, y0 + 120), 90))
    shrink = body.filter(ImageFilter.MinFilter(sw * 2 + 1)) if not small else body.filter(ImageFilter.MinFilter(sw * 2 - 1))
    outline = ImageChops.subtract(body, shrink)
    glyph = outline
    # screen: a white bezel outline with a filled screen inside
    scr = (352, 388, S - 352, 648)
    bez = ImageChops.subtract(rr(scr, 40), rr((scr[0] + sw, scr[1] + sw, scr[2] - sw, scr[3] - sw), 20))
    glyph = ImageChops.lighter(glyph, bez)
    inner = (scr[0] + sw + 16, scr[1] + sw + 16, scr[2] - sw - 16, scr[3] - sw - 16)
    glyph = ImageChops.lighter(glyph, rr(inner, 10).point(lambda v: v * 200 // 255) if not small else rr(inner, 10))
    # D-pad, A and B, start/select
    d = Image.new("L", (S, S), 0); dd = ImageDraw.Draw(d)
    cx, cy, a, wd = 262, 505, 62, 36
    dd.rounded_rectangle((cx - a, cy - wd // 2, cx + a, cy + wd // 2), 8, fill=255)
    dd.rounded_rectangle((cx - wd // 2, cy - a, cx + wd // 2, cy + a), 8, fill=255)
    r = 32 if not small else 42
    for (bx, by) in ((S - 312, 535), (S - 240, 472)):
        dd.ellipse((bx - r, by - r, bx + r, by + r), fill=255)
    if not small:
        for y in (598, 636):
            dd.rounded_rectangle((222, y, 290, y + 16), 8, fill=255)
    glyph = ImageChops.lighter(glyph, d)
    if not small:   # faint shadow so the glyph lifts off the tile
        sh = Image.new("L", (S, S), 0); sh.paste(glyph.filter(ImageFilter.GaussianBlur(10)), (0, 10))
        im = over(im, (70, 0, 80), sh, 90)
    if small:
        bodyf = rr((x0 - 20, y0 - 10, x1 + 20, y1 + 10), 150)
        cut = rr((340, 380, S - 340, 660), 30)
        glyph = ImageChops.subtract(bodyf, cut)
    im = over(im, WHITE, glyph)
    if small:   # a light screen in the window
        im = over(im, (255, 200, 250), rr((370, 410, S - 370, 630), 16))
    return im.resize((size, size), Image.LANCZOS)

out = sys.argv[1]
for s in (256, 128, 64, 48, 32, 24, 16):
    draw(s).save(f"{out}/icon{s}.png")
