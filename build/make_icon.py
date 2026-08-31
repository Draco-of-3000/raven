#!/usr/bin/env python3
"""Generate Raven's app icon: the Phosphor "Bird" mark in white on a black
macOS squircle. Produces build/appicon.png (1024) and a macOS .icns via iconutil.

The icon follows Apple's icon grid: the rounded-rect "body" occupies the safe
area with transparent padding around it, and the corner radius is the standard
~22.37% of the body width (the macOS continuous-rounded-rect proportion), so it
does not read boxier than neighboring dock icons.
"""
import os
import subprocess
from PIL import Image, ImageDraw

HERE = os.path.dirname(os.path.abspath(__file__))
SIZE = 1024
SS = 4  # supersample factor for crisp edges

# Apple macOS icon proportions (fraction of the full canvas).
# The Big Sur+ icon grid: body ~824/1024 of canvas, corner radius ~185.4/824 of
# the body, which gives the familiar rounded-square (not too round, not boxy).
BODY_FRAC = 0.805         # rounded square fills ~80.5% of the canvas (rest is padding)
RADIUS_FRAC = 0.225       # corner radius as fraction of body size

BG = (10, 11, 13, 255)    # near-black, matches Graphite paper
FG = (247, 246, 243, 255) # warm white, matches Parchment ink-on-light inverted

# Phosphor "Bird" path (viewBox 0 0 256 256), MIT licensed. See ATTRIBUTION.md.
BIRD_PATH = ("M236.44,73.34,213.21,57.86A60,60,0,0,0,156,16h-.29C122.79,16.16,96,43.47,"
             "96,76.89V96.63L11.63,197.88l-.1.12A16,16,0,0,0,24,224h88A104.11,104.11,0,0,0,"
             "216,120V100.28l20.44-13.62a8,8,0,0,0,0-13.32ZM126.15,133.12l-60,72a8,8,0,1,1,"
             "-12.29-10.24l60-72a8,8,0,1,1,12.29,10.24ZM164,80a12,12,0,1,1,12-12A12,12,0,0,1,164,80Z")


def parse_path(d):
    """Minimal SVG path parser for the subset used by the Phosphor bird:
    M, C, A, L, V, H, Z (absolute) and their relative forms. Returns a list of
    subpaths, each a list of (x, y) points (arcs/curves flattened)."""
    import re
    tokens = re.findall(r"[MmLlHhVvCcSsAaZz]|-?\d*\.?\d+(?:e-?\d+)?", d)
    i = 0
    subpaths, cur = [], []
    x = y = 0.0
    start = (0.0, 0.0)
    cmd = None

    def num():
        nonlocal i
        v = float(tokens[i]); i += 1; return v

    while i < len(tokens):
        t = tokens[i]
        if re.match(r"[A-Za-z]", t):
            cmd = t; i += 1
        rel = cmd.islower()
        c = cmd.upper()
        if c == "M":
            nx, ny = num(), num()
            x, y = (x + nx, y + ny) if rel else (nx, ny)
            if cur: subpaths.append(cur)
            cur = [(x, y)]; start = (x, y); cmd = "l" if rel else "L"
        elif c == "L":
            nx, ny = num(), num()
            x, y = (x + nx, y + ny) if rel else (nx, ny)
            cur.append((x, y))
        elif c == "H":
            nx = num(); x = x + nx if rel else nx; cur.append((x, y))
        elif c == "V":
            ny = num(); y = y + ny if rel else ny; cur.append((x, y))
        elif c == "C":
            x1, y1, x2, y2, nx, ny = num(), num(), num(), num(), num(), num()
            if rel: x1, y1, x2, y2, nx, ny = x+x1, y+y1, x+x2, y+y2, x+nx, y+ny
            cur.extend(_cubic((x, y), (x1, y1), (x2, y2), (nx, ny)))
            x, y = nx, ny
        elif c == "A":
            rx, ry, rot, large, sweep, nx, ny = num(), num(), num(), num(), num(), num(), num()
            if rel: nx, ny = x + nx, y + ny
            cur.extend(_arc((x, y), rx, ry, rot, large, sweep, (nx, ny)))
            x, y = nx, ny
        elif c == "Z":
            if cur: cur.append(start); subpaths.append(cur); cur = []
        else:
            i += 1
    if cur: subpaths.append(cur)
    return subpaths


def _cubic(p0, p1, p2, p3, n=24):
    pts = []
    for k in range(1, n + 1):
        t = k / n; mt = 1 - t
        x = mt**3*p0[0] + 3*mt**2*t*p1[0] + 3*mt*t**2*p2[0] + t**3*p3[0]
        y = mt**3*p0[1] + 3*mt**2*t*p1[1] + 3*mt*t**2*p2[1] + t**3*p3[1]
        pts.append((x, y))
    return pts


def _arc(p0, rx, ry, phi_deg, large, sweep, p1, n=24):
    import math
    if rx == 0 or ry == 0 or p0 == p1:
        return [p1]
    phi = math.radians(phi_deg)
    cosp, sinp = math.cos(phi), math.sin(phi)
    dx, dy = (p0[0]-p1[0])/2, (p0[1]-p1[1])/2
    x1p = cosp*dx + sinp*dy
    y1p = -sinp*dx + cosp*dy
    rx, ry = abs(rx), abs(ry)
    lam = x1p**2/rx**2 + y1p**2/ry**2
    if lam > 1:
        s = math.sqrt(lam); rx *= s; ry *= s
    denom = rx**2*y1p**2 + ry**2*x1p**2
    num = max(rx**2*ry**2 - denom, 0)
    co = math.sqrt(num/denom) if denom else 0
    if large == sweep: co = -co
    cxp = co*rx*y1p/ry
    cyp = -co*ry*x1p/rx
    cx = cosp*cxp - sinp*cyp + (p0[0]+p1[0])/2
    cy = sinp*cxp + cosp*cyp + (p0[1]+p1[1])/2

    def ang(ux, uy, vx, vy):
        d = math.hypot(ux, uy)*math.hypot(vx, vy)
        c = max(-1, min(1, (ux*vx+uy*vy)/d))
        a = math.acos(c)
        if ux*vy - uy*vx < 0: a = -a
        return a
    th1 = ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
    dth = ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
    if not sweep and dth > 0: dth -= 2*math.pi
    if sweep and dth < 0: dth += 2*math.pi
    pts = []
    for k in range(1, n+1):
        th = th1 + dth*k/n
        x = cosp*rx*math.cos(th) - sinp*ry*math.sin(th) + cx
        y = sinp*rx*math.cos(th) + cosp*ry*math.sin(th) + cy
        pts.append((x, y))
    return pts


def rounded_mask(size, radius):
    m = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(m)
    d.rounded_rectangle([0, 0, size-1, size-1], radius=radius, fill=255)
    return m


def main():
    big = SIZE * SS
    body = int(big * BODY_FRAC)
    pad = (big - body) // 2
    radius = int(body * RADIUS_FRAC)

    # Black squircle body on transparent canvas.
    canvas = Image.new("RGBA", (big, big), (0, 0, 0, 0))
    plate = Image.new("RGBA", (body, body), BG)
    plate.putalpha(rounded_mask(body, radius))
    canvas.paste(plate, (pad, pad), plate)

    # Draw the bird centered on the plate, scaled to ~58% of the body.
    bird_layer = Image.new("RGBA", (big, big), (0, 0, 0, 0))
    bd = ImageDraw.Draw(bird_layer)
    target = body * 0.62
    scale = target / 256.0
    ox = pad + (body - target) / 2
    oy = pad + (body - target) / 2
    for sp in parse_path(BIRD_PATH):
        poly = [(ox + px*scale, oy + py*scale) for px, py in sp]
        if len(poly) >= 3:
            bd.polygon(poly, fill=FG)
    # The path uses even-odd holes (eye + leg). Punch the small eye hole back to BG.
    # Eye center ~ (164,68) r~12 in viewBox space; redraw as background-colored dot.
    ex, ey, er = ox + 168*scale, oy + 68*scale, 13*scale
    bd.ellipse([ex-er, ey-er, ex+er, ey+er], fill=BG)

    out = Image.alpha_composite(canvas, bird_layer)
    out = out.resize((SIZE, SIZE), Image.LANCZOS)
    appicon = os.path.join(HERE, "appicon.png")
    out.save(appicon)
    print("wrote", appicon)

    # Build a macOS .icns for a crisp dock/Finder icon.
    iconset = os.path.join(HERE, "raven.iconset")
    os.makedirs(iconset, exist_ok=True)
    specs = [(16, "16x16"), (32, "16x16@2x"), (32, "32x32"), (64, "32x32@2x"),
             (128, "128x128"), (256, "128x128@2x"), (256, "256x256"),
             (512, "256x256@2x"), (512, "512x512"), (1024, "512x512@2x")]
    for px, label in specs:
        out.resize((px, px), Image.LANCZOS).save(os.path.join(iconset, f"icon_{label}.png"))
    try:
        subprocess.run(["iconutil", "-c", "icns", iconset, "-o", os.path.join(HERE, "raven.icns")], check=True)
        print("wrote raven.icns")
    except Exception as e:
        print("iconutil failed:", e)


if __name__ == "__main__":
    main()
