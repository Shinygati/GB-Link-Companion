"""The cartridge shell for the ROM list: an original flat drawing of a GBA-shaped cartridge, seen from
the front (no wordmarks). The app paints each game's label (colour + initials) into the label window.
Writes assets/cart.png and prints the label window as fractions of the image, for gui.go."""
from PIL import Image, ImageDraw, ImageFilter, ImageChops
import sys
K = 4
W, H = 400, 250                  # output size (aspect 1.6, like the real cartridge)
w, h = W * K, H * K
SHELL, SHELL_HI, SHELL_LO, EDGE = (112, 112, 118), (132, 132, 139), (92, 92, 98), (66, 66, 72)
LABEL = (238, 238, 240)

def poly_mask(points):
    m = Image.new("L", (w, h), 0); ImageDraw.Draw(m).polygon(points, fill=255); return m
def rr(box, r):
    m = Image.new("L", (w, h), 0); ImageDraw.Draw(m).rounded_rectangle(box, r, fill=255); return m

# outline: the top runs across most of the width, and drops by a step at both ends
step_y, step_w, r = int(h * .085), int(w * .055), int(h * .07)
body = ImageChops.lighter(rr((0, step_y, w - 1, h - 1), r), rr((step_w, 0, w - 1 - step_w, h - 1), r))
im = Image.new("RGBA", (w, h), (0, 0, 0, 0))
def fill(mask, col, a=255):
    global im
    layer = Image.new("RGBA", (w, h), col + (0,)); layer.putalpha(mask.point(lambda v: v * a // 255))
    im = Image.alpha_composite(im, layer)
fill(body, EDGE)                                                     # rim / bottom edge
inner = ImageChops.lighter(rr((K * 3, step_y + K * 3, w - 1 - K * 3, h - 1 - K * 9), r - K * 3),
                           rr((step_w + K * 3, K * 3, w - 1 - step_w - K * 3, h - 1 - K * 9), r - K * 3))
fill(inner, SHELL)
# the grip across the top: the area above a curve that rises from both sides towards the middle
import math
pts = []
for i in range(101):
    t = i / 100
    x = w * (.13 + .74 * t)
    y = h * (.235 - .17 * math.sin(math.pi * t) ** .8)
    pts.append((x, y))
# the crescent under the curve is a shallow recess, a little darker than the shell
grip = poly_mask(pts + [(w * .87, h * .25), (w * .13, h * .25)])
grip = ImageChops.multiply(grip, inner)
fill(grip, (102, 102, 108))
line = Image.new("L", (w, h), 0)
ImageDraw.Draw(line).line(pts, fill=255, width=K * 3, joint="curve")
fill(ImageChops.multiply(line, inner), SHELL_LO)
# label window: a recess with a darker lip, then the label
lx0, ly0, lx1, ly1 = .095, .255, .855, .845
lb = (int(w * lx0), int(h * ly0), int(w * lx1), int(h * ly1))
fill(rr((lb[0] - K * 5, lb[1] - K * 5, lb[2] + K * 5, lb[3] + K * 5), K * 9), SHELL_LO)
fill(rr(lb, K * 6), LABEL)
# the small triangle under the label
tx, ty = w * .48, h * .875
fill(poly_mask([(tx - w * .045, ty), (tx + w * .045, ty), (tx, ty + h * .045)]), SHELL_LO)
# gentle highlight along the top edge
hl = rr((step_w + K * 6, K * 4, w - step_w - K * 6, K * 9), K * 3).filter(ImageFilter.GaussianBlur(K))
fill(hl, (255, 255, 255), 60)
im.resize((W, H), Image.LANCZOS).save(sys.argv[1])
print(f"label window: x {lx0}..{lx1}, y {ly0}..{ly1}, radius {6 / H:.4f} of height")
