#!/usr/bin/env python3
"""Builds docs/blog.html from docs/BLOG.md.

Run from the repository root: python3 docs/mkblog.py

It handles the Markdown that BLOG.md uses: headings, paragraphs, bullet and
numbered lists, fenced code, tables, inline code, bold, italics, and links.
It also inserts the leaf diagram before "The numbers fit together on purpose".
"""
import html
import re

HEAD = '<!doctype html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n<meta name="viewport" content="width=device-width, initial-scale=1">\n<title>Porting bbtree to Go</title>\n<style>\n:root {\n\t--bg: #ffffff; --fg: #1b1f24; --muted: #5b6572; --rule: #dde2e8;\n\t--accent: #2346c8; --code-bg: #f2f4f7;\n\t--seg-hdr: #3b4554; --seg-ik: #2346c8; --seg-nxt: #9aa4b2; --seg-key: #2b8a7e; --seg-val: #c9852a; --seg-slk: #dde2e8;\n\tcolor-scheme: light;\n}\n@media (prefers-color-scheme: dark) {\n\t:root {\n\t\t--bg: #111418; --fg: #e3e7ed; --muted: #98a2b0; --rule: #2a3039;\n\t\t--accent: #8ea4ff; --code-bg: #181d24;\n\t\t--seg-hdr: #b8c1cd; --seg-ik: #7f97ff; --seg-nxt: #5d6876; --seg-key: #4cb3a5; --seg-val: #e0a14d; --seg-slk: #2a3039;\n\t\tcolor-scheme: dark;\n\t}\n}\n* { box-sizing: border-box; }\nbody {\n\tmargin: 0; background: var(--bg); color: var(--fg);\n\tfont: 17px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;\n\tpadding: 40px 16px 80px;\n}\nmain { max-width: 72ch; margin: 0 auto; }\nh1 { font-size: 2.2em; line-height: 1.15; margin: 0 0 8px; }\n.dek { color: var(--muted); font-style: italic; margin: 0 0 32px; }\nh2 { font-size: 1.4em; line-height: 1.25; margin: 2em 0 0.6em; padding-top: 0.6em; border-top: 1px solid var(--rule); }\na { color: var(--accent); }\ncode { font: 0.86em ui-monospace, "SF Mono", Menlo, Consolas, monospace; background: var(--code-bg); padding: 0.1em 0.3em; border-radius: 3px; overflow-wrap: anywhere; }\nfigure { margin: 1.5em 0; }\nfigure.code { background: var(--code-bg); border: 1px solid var(--rule); border-radius: 6px; }\nfigure.code figcaption { font-size: 12px; color: var(--muted); padding: 6px 14px; border-bottom: 1px solid var(--rule); }\nfigure.code pre { margin: 0; padding: 14px; overflow-x: auto; tab-size: 4; }\nfigure.code pre code { background: none; padding: 0; font-size: 13.5px; line-height: 1.55; overflow-wrap: normal; }\n.c { color: var(--muted); }\n.table { overflow-x: auto; margin: 1.5em 0; }\ntable { border-collapse: collapse; font-size: 14.5px; font-variant-numeric: tabular-nums; }\nth, td { text-align: left; padding: 7px 12px; border-bottom: 1px solid var(--rule); white-space: nowrap; }\nth { color: var(--muted); font-weight: 600; }\nli { margin-bottom: 0.4em; }\nnav ol { padding-left: 1.4em; }\nfigure.leafmap { border: 1px solid var(--rule); border-radius: 6px; padding: 16px; }\nfigure.leafmap figcaption { display: grid; gap: 2px; margin-bottom: 12px; }\n.fig-title { font-weight: 600; }\n.fig-note { font-size: 14px; color: var(--muted); }\n.lines { display: grid; gap: 3px; }\n.line { display: grid; grid-template-columns: 4.2ch 1fr; gap: 8px; align-items: center; }\n.off { font: 11px ui-monospace, Menlo, Consolas, monospace; color: var(--muted); text-align: right; white-space: pre; }\n.bar { display: flex; height: 14px; gap: 1px; outline: 2px solid transparent; outline-offset: 2px; }\n.line.hot .bar { outline-color: var(--accent); }\n.seg, .sw { display: block; }\n.hdr { background: var(--seg-hdr); } .ik { background: var(--seg-ik); } .nxt { background: var(--seg-nxt); }\n.key { background: var(--seg-key); } .val { background: var(--seg-val); }\n.slk { background: repeating-linear-gradient(135deg, var(--seg-slk) 0 3px, transparent 3px 6px); border: 1px solid var(--seg-slk); }\n.legend { list-style: none; padding: 0; margin: 14px 0 0; display: flex; flex-wrap: wrap; gap: 6px 18px; font-size: 14px; color: var(--muted); }\n.legend li { display: flex; align-items: center; gap: 6px; margin: 0; }\n.sw { width: 12px; height: 12px; flex: none; }\n</style>\n</head>\n<body>\n'

# A uint64 -> uint64 leaf, padded to 1,024 bytes in the slab: header 0-16,
# inline keys 16-128, next 128-136, keys 136-576, values 576-1016, padding
# 1016-1024.
SEGMENTS = [(0, 16, "hdr"), (16, 128, "ik"), (128, 136, "nxt"), (136, 576, "key"),
            (576, 1016, "val"), (1016, 1024, "slk")]


def diagram():
    rows = []
    for line in range(16):
        a, b = line * 64, line * 64 + 64
        cells = ""
        for s, e, k in SEGMENTS:
            lo, hi = max(a, s), min(b, e)
            if lo < hi:
                cells += f'<span class="seg {k}" style="flex:{hi - lo}" title="bytes {lo} to {hi}"></span>'
        hot = " hot" if line < 2 else ""
        rows.append(f'<div class="line{hot}"><span class="off">{a:>4}</span><span class="bar">{cells}</span></div>')
    return ("""<figure class="leafmap" aria-label="Byte map of a uint64 to uint64 leaf, one row per 64-byte cache line">
<figcaption><span class="fig-title">One <code>uint64 -&gt; uint64</code> leaf, one row per 64-byte cache line</span>
<span class="fig-note">The search reads the two marked lines. A hit then reads one line of keys and one of values.</span></figcaption>
<div class="lines">""" + "\n".join(rows) + """</div>
<ul class="legend">
<li><span class="sw hdr"></span>header, 16 B</li>
<li><span class="sw ik"></span>56 inline keys, 112 B</li>
<li><span class="sw nxt"></span><code>next</code>, 8 B</li>
<li><span class="sw key"></span>55 keys, 440 B</li>
<li><span class="sw val"></span>55 values, 440 B</li>
<li><span class="sw slk"></span>slab padding to 1,024 B, 8 B</li>
</ul>
</figure>""")


def slug(t):
    t = re.sub(r"<[^>]+>", "", t)
    return re.sub(r"[^a-z0-9]+", "-", t.lower()).strip("-")


def inline(t):
    codes = []

    def stash(m):
        codes.append("<code>" + html.escape(m.group(1), quote=False) + "</code>")
        return f"\x00{len(codes) - 1}\x00"

    t = re.sub(r"`([^`]+)`", stash, t)
    t = html.escape(t, quote=False)

    def link(m):
        text, href = m.group(1), m.group(2)
        if href.startswith("http") or href.startswith("#"):
            return f'<a href="{href}">{text}</a>'
        # Links in BLOG.md are relative to docs/, as blog.html is.
        return f'<a href="{href}">{text}</a>'

    t = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", link, t)
    t = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", t)
    t = re.sub(r"(?<![*\w])\*([^*]+)\*(?![*\w])", r"<em>\1</em>", t)
    return re.sub(r"\x00(\d+)\x00", lambda m: codes[int(m.group(1))], t)


def convert(src):
    out, toc, para = [], [], []
    title = dek = ""

    def flush():
        if para:
            out.append("<p>" + inline(" ".join(para)) + "</p>")
            para.clear()

    i, n = 0, len(src)
    while i < n:
        line = src[i]
        if line.startswith("```"):
            flush()
            lang = line[3:].strip()
            i += 1
            buf = []
            while not src[i].startswith("```"):
                buf.append(src[i])
                i += 1
            i += 1
            code = html.escape("\n".join(buf), quote=False)
            if lang == "go":
                code = re.sub(r"(//[^\n]*)", r'<span class="c">\1</span>', code)
            elif lang in ("bash", "text"):
                code = re.sub(r"(^|\s)(#[^\n]*)", r'\1<span class="c">\2</span>', code)
            label = {"go": "Go", "bash": "shell", "text": "layout"}.get(lang, lang)
            out.append(f'<figure class="code"><figcaption>{label}</figcaption><pre><code>{code}</code></pre></figure>')
            continue
        if line.startswith("# "):
            title = inline(line[2:])
            i += 1
            continue
        if not dek and line.startswith("*") and line.endswith("*") and not line.startswith("**"):
            buf = [line]
            dek = inline(line.strip("*"))
            i += 1
            continue
        if not dek and line.startswith("*") and not line.startswith("**"):
            buf = [line]
            i += 1
            while not buf[-1].endswith("*"):
                buf.append(src[i])
                i += 1
            dek = inline(" ".join(buf).strip("*"))
            continue
        m = re.match(r"^(#{2,3}) (.*)", line)
        if m:
            flush()
            lvl, text = len(m.group(1)), inline(m.group(2))
            sid = slug(text)
            if lvl == 2:
                toc.append((sid, text))
            out.append(f'<h{lvl} id="{sid}">{text}</h{lvl}>')
            i += 1
            continue
        if line.startswith("|"):
            flush()
            rows = []
            while i < n and src[i].startswith("|"):
                rows.append([c.strip() for c in src[i].strip().strip("|").split("|")])
                i += 1
            t = '<div class="table"><table><thead><tr>' + "".join(f"<th>{inline(c)}</th>" for c in rows[0]) + "</tr></thead><tbody>"
            for r in rows[2:]:
                t += "<tr>" + "".join(f"<td>{inline(c)}</td>" for c in r) + "</tr>"
            out.append(t + "</tbody></table></div>")
            continue
        m = re.match(r"^(- |\d+\. )(.*)", line)
        if m:
            flush()
            ordered = m.group(1)[0].isdigit()
            items = []
            while i < n and re.match(r"^(- |\d+\. )", src[i]):
                cur = [re.sub(r"^(- |\d+\. )", "", src[i])]
                i += 1
                while i < n and src[i].startswith("  ") and src[i].strip():
                    cur.append(src[i].strip())
                    i += 1
                items.append(" ".join(cur))
            tag = "ol" if ordered else "ul"
            out.append(f"<{tag}>" + "".join(f"<li>{inline(x)}</li>" for x in items) + f"</{tag}>")
            continue
        if not line.strip():
            flush()
            i += 1
            continue
        para.append(line.strip())
        i += 1
    flush()
    return title, dek, toc, "\n".join(out)


def main():
    title, dek, toc, body = convert(open("docs/BLOG.md").read().split("\n"))
    marker = "<p>The numbers fit together on purpose:</p>"
    assert body.count(marker) == 1, "diagram anchor missing from BLOG.md"
    body = body.replace(marker, diagram() + "\n" + marker)
    nav = "\n".join(f'<li><a href="#{s}">{t}</a></li>' for s, t in toc)
    page = (HEAD + f'<main>\n<h1>{title}</h1>\n<p class="dek">{dek}</p>\n'
            f'<nav aria-label="Contents">\n<strong>Contents</strong>\n<ol>\n{nav}\n</ol>\n</nav>\n'
            f"{body}\n</main>\n</body>\n</html>\n")
    open("docs/blog.html", "w").write(page)


if __name__ == "__main__":
    main()
