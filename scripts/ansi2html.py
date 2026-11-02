#!/usr/bin/env python3
"""Convert a true-colour ANSI screen (lipgloss output) to HTML spans for the docs."""
import html, re, sys

SGR = re.compile(r"\x1b\[([0-9;]*)m")


def xterm256(n):
    """xterm 256-colour index → #rrggbb."""
    base = ["#000000", "#cd0000", "#00cd00", "#cdcd00", "#0000ee", "#cd00cd", "#00cdcd", "#e5e5e5",
            "#7f7f7f", "#ff0000", "#00ff00", "#ffff00", "#5c5cff", "#ff00ff", "#00ffff", "#ffffff"]
    if n < 16:
        return base[n]
    if n < 232:
        n -= 16
        lv = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (lv[n // 36], lv[(n // 6) % 6], lv[n % 6])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)

def convert(text):
    out, fg, bg, bold, italic = [], None, None, False, False
    pos = 0
    def emit(s):
        if not s:
            return
        style = []
        if fg: style.append(f"color:{fg}")
        if bg: style.append(f"background:{bg}")
        if bold: style.append("font-weight:700")
        if italic: style.append("font-style:italic")
        s = html.escape(s)
        out.append(f'<span style="{";".join(style)}">{s}</span>' if style else s)
    for m in SGR.finditer(text):
        emit(text[pos:m.start()])
        pos = m.end()
        codes = [int(c) if c else 0 for c in m.group(1).split(";")]
        i = 0
        while i < len(codes):
            c = codes[i]
            if c == 0: fg = bg = None; bold = italic = False
            elif c == 1: bold = True
            elif c == 3: italic = True
            elif c == 22: bold = False
            elif c == 23: italic = False
            elif c == 39: fg = None
            elif c == 49: bg = None
            elif c in (38, 48) and i + 2 < len(codes) and codes[i + 1] == 5:
                col = xterm256(codes[i + 2])
                if c == 38: fg = col
                else: bg = col
                i += 2
            elif c in (38, 48) and i + 4 < len(codes) and codes[i + 1] == 2:
                col = "#%02x%02x%02x" % tuple(codes[i + 2:i + 5])
                if c == 38: fg = col
                else: bg = col
                i += 4
            i += 1
    emit(text[pos:])
    return "".join(out)

src = open(sys.argv[1], encoding="utf-8").read()
src = re.sub(r"\x1b\[[0-9;?]*[A-Za-ln-z]", "", src)  # drop non-SGR sequences
print('<pre class="tui">' + convert(src) + "</pre>")
