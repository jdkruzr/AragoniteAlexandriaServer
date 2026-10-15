#!/usr/bin/env python3
"""Wrap owner-facing text in Go html templates with {{t "..."}}.

Plain text segments and the aria-label/placeholder/title/alt attributes become
translatable messages whose ID is the English text (entities decoded). Segments
next to a printed value ({{.X}}) are reported instead of wrapped: piecemeal
fragments would freeze English word order, so those are rewritten by hand with
placeholders. Product names, code-like tokens and <script>/<style>/<code> are
left alone. Usage: i18n-wrap.py [--dry-run] FILE...
"""
import html, re, sys

KEEP = {"Alexandria", "Alexandria Server", "Alexandria Client", "BOOX", "PowerSync", "ksync", "MCP", "CalDAV", "OAuth", "OCR", "API", "URL", "PDF", "EPUB", "MP3", "PNG", "JSON", "UTC", "ID"}
ATTRS = ("aria-label", "placeholder", "title", "alt")
# A template action; quoted strings inside it may themselves contain "{{ }}".
ACT = r'\{\{(?:"(?:\\.|[^"\\])*"|[^"}]|\}(?!\}))*\}\}'
ACTION = re.compile(r"\{\{-?\s*(.*?)\s*-?\}\}$", re.S)
CONTROL = re.compile(r"^(if|else|end|range|with|define|template|block|break|continue|/\*|\$\w+\s*:?=)")


def go_string(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n") + '"'


def translatable(text):
    t = " ".join(html.unescape(text).split())
    if not re.search(r"[A-Za-z]{2}", t) or t in KEEP:
        return None
    if " " not in t and re.search(r"[/_=.@#:]|^[a-z0-9-]+$", t):  # paths, keys, code
        return None
    return t


# Actions that print no words (sort arrows, help buttons) can sit beside a
# message without splitting a sentence.
WORDLESS = re.compile(r'Indicator "|^template "help"')


def printing(action):
    return not CONTROL.match(action) and not action.startswith("t ") and not WORDLESS.search(action)


def wrap_text(chunk, flagged, where):
    """chunk is text between tags; may contain actions."""
    parts = re.split("(" + ACT + ")", chunk, flags=re.S)
    out = []
    for i, p in enumerate(parts):
        if p.startswith("{{"):
            out.append(p)
            continue
        msg = translatable(p)
        if msg is None:
            out.append(p)
            continue
        near = [parts[j] for j in (i - 1, i + 1) if 0 <= j < len(parts) and parts[j].startswith("{{")]
        if any(printing(ACTION.match(a).group(1)) for a in near if ACTION.match(a)):
            flagged.append((where, chunk.strip()))
            out.append(p)
            continue
        lead = p[: len(p) - len(p.lstrip())]
        trail = p[len(p.rstrip()):]
        out.append(f"{lead}{{{{t {go_string(msg)}}}}}{trail}")
    return "".join(out)


def wrap_attrs(tag):
    def sub(m):
        name, q, val = m.group(1), m.group(2), m.group(3)
        msg = translatable(val) if "{{" not in val else None
        return f"{name}={q}{{{{t {go_string(msg)}}}}}{q}" if msg else m.group(0)
    return re.sub(r'\b(%s)=(["\'])(.*?)\2' % "|".join(ATTRS), sub, tag)


def process(src, name):
    flagged, out, i = [], [], 0
    token = re.compile(r"<(script|style|code|pre)\b.*?</\1>|<!--.*?-->|<(?:" + ACT + r"|[^>{]|\{(?!\{))*>", re.S | re.I)
    for m in token.finditer(src):
        text = src[i:m.start()]
        out.append(wrap_text(text, flagged, name))
        tag = m.group(0)
        out.append(tag if m.group(1) or tag.startswith("<!--") else wrap_attrs(tag))
        i = m.end()
    out.append(wrap_text(src[i:], flagged, name))
    return "".join(out), flagged


if __name__ == "__main__":
    dry = "--dry-run" in sys.argv
    for f in [a for a in sys.argv[1:] if a != "--dry-run"]:
        src = open(f).read()
        new, flagged = process(src, f)
        if not dry and new != src:
            open(f, "w").write(new)
        for where, text in flagged:
            print(f"FLAG {where}: {text[:160]}")
