#!/usr/bin/env python3
"""Generate the Raven dmg background: flat parchment, a soft arrow from the app
icon position toward the Applications folder, and a quiet hint line. Matches the
app's aesthetic (no gradients). Outputs background.png (@1x) and background@2x.png.

The dmg window is 540x380; icons sit on a row at y~170 (app at x~140,
Applications at x~400). The arrow is drawn between them.
"""
import os
from PIL import Image, ImageDraw, ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
W, H = 540, 380

# Parchment + ink, matching the app theme.
BG = (247, 246, 243, 255)
INK = (21, 22, 27)
MUTED = (138, 135, 128)
ARROW = (180, 176, 168)


def load_font(size, bold=False):
    candidates = [
        "/System/Library/Fonts/SFNS.ttf",
        "/System/Library/Fonts/Helvetica.ttc",
        "/Library/Fonts/Arial.ttf",
    ]
    for c in candidates:
        if os.path.exists(c):
            try:
                return ImageFont.truetype(c, size)
            except Exception:
                pass
    return ImageFont.load_default()


APPICON = os.path.join(HERE, "..", "appicon.png")  # the real Raven app icon


def draw(scale):
    w, h = W * scale, H * scale
    img = Image.new("RGBA", (w, h), BG)
    d = ImageDraw.Draw(img)

    def s(v):
        return int(v * scale)

    # The real Raven app icon, centered near the top as the brand mark.
    icon_px = s(64)
    icon = Image.open(APPICON).convert("RGBA").resize((icon_px, icon_px), Image.LANCZOS)
    img.paste(icon, (int((w - icon_px) / 2), s(28)), icon)

    # Wordmark + hint beneath the mark.
    title_font = load_font(s(22))
    hint_font = load_font(s(13))
    title = "Raven"
    tw = d.textlength(title, font=title_font)
    d.text(((w - tw) / 2, s(98)), title, font=title_font, fill=INK)

    sub = "Drag Raven into your Applications folder"
    sw = d.textlength(sub, font=hint_font)
    d.text(((w - sw) / 2, s(130)), sub, font=hint_font, fill=MUTED)

    # Arrow between the two icon slots (icon row centered ~y=210 in window coords,
    # but the Finder icons are drawn ON TOP by create-dmg; we just draw the arrow
    # in the gap between x=140 and x=400).
    y = s(205)
    x0, x1 = s(225), s(315)
    lw = max(2, s(3))
    d.line([(x0, y), (x1, y)], fill=ARROW, width=lw)
    # arrowhead
    ah = s(9)
    d.line([(x1, y), (x1 - ah, y - ah)], fill=ARROW, width=lw)
    d.line([(x1, y), (x1 - ah, y + ah)], fill=ARROW, width=lw)

    out = os.path.join(HERE, "background@2x.png" if scale == 2 else "background.png")
    img.save(out)
    print("wrote", out, f"({w}x{h})")


draw(1)
draw(2)
