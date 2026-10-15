#!/usr/bin/env python3
"""Merge sentence fragments split by inline markup into one HTML message.

After i18n-wrap.py, prose such as `Check <a href="/jobs">Recognition</a> first.`
becomes three messages that can't be translated in isolation. For each block
element whose content is only {{t "…"}} actions (no arguments), static inline
markup and punctuation, this replaces the content with a single
{{th "<html>"}} message: translators see the whole sentence, markup included.
Blocks with template actions in attributes or other actions are left alone.
Usage: i18n-merge.py FILE...
"""
import html, re, sys

BLOCK = "p|li|dd|dt|td|th|summary|h1|h2|h3|h4|label|figcaption|blockquote|span|div|small"
INLINE = {"a", "strong", "em", "code", "kbd", "b", "i", "br", "abbr", "small", "span"}
Q = r'"(?:\\.|[^"\\])*"'
T_ACTION = re.compile(r"\{\{t (" + Q + r")\}\}")
inner_block = re.compile(r"<(" + BLOCK + r")(\s[^>{]*)?>((?:(?!<(?:" + BLOCK + r"|ul|ol|table|section|form|nav|details)\b)(?!</\1>).)*?)</\1>", re.S)


def go_unquote(q):
    return q[1:-1].encode().decode("unicode_escape").encode("latin-1").decode("utf-8")


def go_quote(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n") + '"'


def merge(content):
    if len(T_ACTION.findall(content)) < 1:
        return None
    rest = T_ACTION.sub("", content)
    if "{{" in rest:  # other template actions: leave alone
        return None
    tags = re.findall(r"</?([a-zA-Z0-9]+)", rest)
    if not tags or any(t.lower() not in INLINE for t in tags):
        return None  # nothing to merge, or non-inline markup
    text = T_ACTION.sub(lambda m: html.escape(go_unquote(m.group(1)), quote=False), content)
    lead = text[: len(text) - len(text.lstrip())]
    trail = text[len(text.rstrip()):]
    body = " ".join(text.split())
    return f"{lead}{{{{th {go_quote(body)}}}}}{trail}"


def process(src):
    changed = 0

    def sub(m):
        nonlocal changed
        new = merge(m.group(3))
        if new is None:
            return m.group(0)
        changed += 1
        return f"<{m.group(1)}{m.group(2) or ''}>{new}</{m.group(1)}>"

    # Innermost blocks first; repeat so a merged span can join its paragraph.
    for _ in range(3):
        src = inner_block.sub(sub, src)
    return src, changed


if __name__ == "__main__":
    for f in sys.argv[1:]:
        s = open(f).read()
        new, n = process(s)
        if n:
            open(f, "w").write(new)
            print(f"{f}: merged {n}")
