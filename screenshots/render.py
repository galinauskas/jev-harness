#!/usr/bin/env python3
"""Render TestShowcase ANSI views to PNG. Requires Pillow and a monospace font."""
import argparse
from pathlib import Path
import re
from PIL import Image, ImageDraw, ImageFont

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--font', default='/System/Library/Fonts/Menlo.ttc')
args = parser.parse_args()
root = Path(__file__).resolve().parent
font = ImageFont.truetype(args.font, 16)
small = ImageFont.truetype(args.font, 13)
cw, ch = round(font.getlength('M')), 23
pad, top = 24, 64
width, height = 112 * cw + pad * 2, 34 * ch + top + 58
base = (20, 23, 29)
normal = (220, 224, 232)
palette = [(0,0,0),(205,49,49),(13,188,121),(229,229,16),(36,114,200),(188,63,188),(17,168,205),(229,229,229),(102,102,102),(241,76,76),(35,209,139),(245,245,67),(59,142,234),(214,112,214),(41,184,219),(255,255,255)]
def colour(n):
    if n < 16: return palette[n]
    if n >= 232: return (8 + (n-232)*10,) * 3
    n -= 16
    levels = [0,95,135,175,215,255]
    return levels[n//36], levels[n//6%6], levels[n%6]

for src in sorted((root / '.raw').glob('*.ansi')):
    im = Image.new('RGB', (width, height), base)
    draw = ImageDraw.Draw(im)
    draw.text((pad, 18), 'jev-harness  /  ' + src.stem, font=small, fill=(153,168,191))
    draw.line((pad, 45, width-pad, 45), fill=(55,62,74))
    fg, bg, bold = normal, base, False
    for row, line in enumerate(src.read_text().split('\n')):
        x, y = pad, top + row*ch
        for token in re.split(r'(\x1b\[[0-9;:]*m)', line):
            if token.startswith('\x1b['):
                codes = [int(v or 0) for v in token[2:-1].replace(':',';').split(';')]
                i = 0
                while i < len(codes):
                    n = codes[i]
                    if n == 0: fg, bg, bold = normal, base, False
                    elif n == 1: bold = True
                    elif n == 22: bold = False
                    elif n == 39: fg = normal
                    elif n == 49: bg = base
                    elif 30 <= n <= 37: fg = palette[n-30]
                    elif 90 <= n <= 97: fg = palette[n-90+8]
                    elif 40 <= n <= 47: bg = palette[n-40]
                    elif n in (38,48) and i+2 < len(codes):
                        if codes[i+1] == 5: col = colour(codes[i+2]); i += 2
                        elif codes[i+1] == 2 and i+4 < len(codes): col = tuple(codes[i+2:i+5]); i += 4
                        else: i += 1; continue
                        if n == 38: fg = col
                        else: bg = col
                    i += 1
                continue
            for char in token:
                draw.rectangle((x,y,x+cw-1,y+ch-1), fill=bg)
                draw.text((x,y), char, font=font, fill=fg)
                if bold: draw.text((x+0.4,y), char, font=font, fill=fg)
                x += cw
    draw.line((pad, height-43, width-pad, height-43), fill=(55,62,74))
    caption = 'Current TUI renderer | Demo data | No live model requests | 30 September 2026'
    if src.stem.startswith(('27-', '28-')):
        caption = 'Current TUI renderer | Actual staged operation on disposable files | 30 September 2026'
    draw.text((pad,height-30), caption, font=small, fill=(153,168,191))
    im.save(root / (src.stem + '.png'))
    with Image.open(root / (src.stem + '.png')) as check: check.verify()

# Overview uses the same rendered routing captures, without invented charts.
files = ['02-auto-routing-and-code','03-complex-task-routing','04-confidence-fallback','05-pinned-role']
thumbs = []
for name in files:
    im = Image.open(root / (name+'.png'))
    im.thumbnail((800,650))
    thumbs.append(im.copy())
w, h = thumbs[0].size
board = Image.new('RGB',(w*2+18,h*2+18),base)
for i, im in enumerate(thumbs): board.paste(im,((i%2)*(w+18),(i//2)*(h+18)))
board.save(root / 'routing-overview.png')
print(f'Rendered {len(list((root / ".raw").glob("*.ansi"))) + 1} PNGs.')
