#!/usr/bin/env python3
"""Build the gooseneck website into site/dist/.

No dependencies: pages are HTML fragments, this script wraps them in the
layout, builds the docs sidebar / table of contents / search index, highlights
code blocks and pulls the theme palettes straight from the Go source so the
site's theme switcher always matches the TUI.

    python3 site/build.py          # writes site/dist/
"""

import html
import json
import re
import shutil
from pathlib import Path

SITE = Path(__file__).resolve().parent
REPO = SITE.parent
DIST = SITE / "dist"
REPO_URL = "https://github.com/lubabs770/gooseneck"
INSTALL = "curl -fsSL https://raw.githubusercontent.com/lubabs770/gooseneck/main/install.sh | sh"


# ---------------------------------------------------------------- palettes

def palettes():
    """Parse the `palettes` map out of player/internal/tui/theme.go."""
    src = (REPO / "player/internal/tui/theme.go").read_text()
    out = {}
    for name, body in re.findall(r'"(\w+)":\s*\{(.*?)\},\n', src, re.S):
        fields = dict(re.findall(r'(\w+):\s*"(#[0-9a-fA-F]{6})"', body))
        if "bg" in fields and "primary" in fields:
            fields["light"] = "light: true" in body
            out[name] = fields
    order = ["opencode", "tokyonight", "catppuccin", "gruvbox", "nord", "everforest",
             "kanagawa", "rosepine", "onedark", "dracula", "ayu", "matrix", "mono", "latte"]
    return {k: out[k] for k in order if k in out} | {k: v for k, v in out.items() if k not in order}


def catalog_stats():
    data = json.loads((REPO / "artists.json").read_text())
    artists = data["artists"]
    tags = {
        "Chasidish": "isChasid", "Israeli": "isIsraeli", "American": "isAmerican",
        "Famous": "isFamous", "Female vocalists": "isFemale", "Kids": "isKidZone",
        "DJs": "isDJ", "A cappella": "isAcapellaOnly",
    }
    famous = [a for a in artists if a.get("isFamous") and a.get("thumbnail")]
    return {
        "count": len(artists),
        "tags": {k: sum(1 for a in artists if a.get(v)) for k, v in tags.items()},
        "famous": [{"name": a["name"], "img": a["thumbnail"]} for a in famous[:48]],
    }


# ---------------------------------------------------------------- highlighting

def hl_sh(code):
    out = []
    for line in code.split("\n"):
        m = re.match(r"^(.*?)(\s+#.*|^#.*)?$", line)
        body, comment = m.group(1), m.group(2) or ""
        if line.lstrip().startswith("#"):
            body, comment = "", line
        b = html.escape(body)
        b = re.sub(r"(&quot;.*?&quot;|&#x27;.*?&#x27;)", r'<span class="s">\1</span>', b)
        b = re.sub(r"(^|\s)(--?[a-zA-Z][\w-]*)", r'\1<span class="f">\2</span>', b)
        b = re.sub(r"(^|\| |\$\(|\s\|\s)(goose|gooseneck-player|curl|go|vhs|jq|fzf|xargs|cd|sh|mpv|yt-dlp|playerctl)\b",
                   r'\1<span class="c">\2</span>', b)
        b = re.sub(r"^(\$ )", r'<span class="p">\1</span>', b)
        out.append(b + (f'<span class="m">{html.escape(comment)}</span>' if comment else ""))
    return "\n".join(out)


def hl_toml(code):
    out = []
    for line in code.split("\n"):
        m = re.match(r"^(\s*)([\w.]+)(\s*=\s*)(.*?)(\s+#.*)?$", line)
        if m:
            ind, key, eq, val, com = m.groups()
            v = html.escape(val)
            v = re.sub(r"(&quot;.*?&quot;)", r'<span class="s">\1</span>', v)
            v = re.sub(r"\b(true|false|\d+)\b", r'<span class="n">\1</span>', v)
            out.append(f'{ind}<span class="k">{key}</span>{eq}{v}'
                       + (f'<span class="m">{html.escape(com)}</span>' if com else ""))
        elif line.strip().startswith("#"):
            out.append(f'<span class="m">{html.escape(line)}</span>')
        elif line.strip().startswith("["):
            m2 = re.match(r"^(\s*\[\[?[^\]]+\]\]?)(.*)$", line)
            out.append(f'<span class="c">{html.escape(m2.group(1))}</span><span class="m">{html.escape(m2.group(2))}</span>')
        else:
            out.append(html.escape(line))
    return "\n".join(out)


def highlight(fragment):
    """<pre data-lang="sh">raw text</pre>  ->  highlighted, with a copy button."""
    def repl(m):
        lang, title, code = m.group(1), m.group(2), m.group(3)
        code = html.unescape(code.strip("\n"))
        body = {"sh": hl_sh, "toml": hl_toml}.get(lang, html.escape)(code)
        head = (f'<div class="code-title"><span>{title}</span></div>' if title else "")
        return (f'<div class="code">{head}<button class="copy" aria-label="Copy">copy</button>'
                f'<pre class="lang-{lang}"><code>{body}</code></pre></div>')
    return re.sub(r'<pre data-lang="(\w+)"(?: data-title="([^"]*)")?>(.*?)</pre>', repl, fragment, flags=re.S)


# ---------------------------------------------------------------- layout

def slugify(text):
    return re.sub(r"[^a-z0-9]+", "-", re.sub(r"<.*?>", "", text).lower()).strip("-")


def layout(title, body, *, desc, depth, page_class, extra_head="", root=None):
    root = "../" * depth if root is None else root
    return f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{html.escape(title)}</title>
<meta name="description" content="{html.escape(desc)}">
<meta property="og:title" content="{html.escape(title)}">
<meta property="og:description" content="{html.escape(desc)}">
<meta name="theme-color" content="#0a0a0a">
<link rel="icon" href="{root}favicon.svg" type="image/svg+xml">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Fraunces:ital,opsz,wght,SOFT@0,9..144,300..800,100;1,9..144,300..800,100&family=JetBrains+Mono:wght@400;500;700&family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
<link rel="stylesheet" href="{root}style.css">
<script>try{{var t=localStorage.getItem("goose-theme");if(t)document.documentElement.dataset.theme=t}}catch(e){{}}</script>
<script src="{root}themes.js"></script>
{extra_head}
</head>
<body class="{page_class}">
{nav(root)}
{body}
{footer(root)}
{palette_dialog()}
<script>window.GOOSE_ROOT={json.dumps(root)};</script>
<script src="{root}app.js" defer></script>
</body>
</html>
"""


LOGO = """<svg class="logo-mark" viewBox="0 0 32 32" aria-hidden="true"><path d="M9 27c0-6 3-9 8-11 4-1.6 6-4 6-7.5C23 5 20.5 3 17.5 3 14.6 3 12.5 5 12.5 7.6" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"/><path d="M12.4 7.4 8 9.2l4.6 1" fill="var(--primary)" stroke="var(--primary)" stroke-width="1.6" stroke-linejoin="round"/><circle cx="16.5" cy="6.6" r="1.3" fill="currentColor"/><path d="M5 27h18" stroke="currentColor" stroke-width="3" stroke-linecap="round"/></svg>"""


def nav(root):
    return f"""<header class="nav">
  <a class="brand" href="{root}">{LOGO}<span>gooseneck</span></a>
  <nav class="nav-links">
    <a href="{root}docs/">Docs</a>
    <a href="{root}docs/cli/">CLI</a>
    <a href="{root}docs/themes/">Themes</a>
    <a href="{REPO_URL}/releases">Releases</a>
  </nav>
  <div class="nav-right">
    <button class="search-btn" data-open-palette aria-label="Search the docs"><span>Search</span><kbd>ctrl</kbd><kbd>k</kbd></button>
    <button class="theme-btn" data-cycle-theme aria-label="Change theme" title="Change theme (t)"><span class="swatch-dot"></span></button>
    <a class="gh" href="{REPO_URL}" aria-label="GitHub"><svg viewBox="0 0 16 16" width="20" height="20"><path fill="currentColor" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg></a>
  </div>
</header>"""


def footer(root):
    return f"""<footer class="footer">
  <div class="footer-inner">
    <a class="brand" href="{root}">{LOGO}<span>gooseneck</span></a>
    <p>Jewish music in your terminal. Free and open source.</p>
    <div class="footer-links">
      <a href="{root}docs/">Docs</a><a href="{REPO_URL}">GitHub</a><a href="{REPO_URL}/releases">Releases</a><a href="{REPO_URL}/issues">Issues</a>
    </div>
  </div>
</footer>"""


def palette_dialog():
    return """<div class="palette" hidden>
  <div class="palette-box" role="dialog" aria-label="Search">
    <div class="palette-head"><span class="palette-title">Commands</span><kbd>esc</kbd></div>
    <input class="palette-input" placeholder="Search docs, or type a theme name…" autocomplete="off" spellcheck="false">
    <ul class="palette-list"></ul>
  </div>
</div>"""


# ---------------------------------------------------------------- docs

def load_docs():
    docs = []
    for f in sorted((SITE / "docs").glob("*.html")):
        text = f.read_text()
        meta = dict(re.findall(r"^<!--\s*(\w+):\s*(.*?)\s*-->", text, re.M))
        body = re.sub(r"^<!--.*?-->\n", "", text, flags=re.M)
        slug = f.stem.split("-", 1)[1]
        docs.append({"slug": "" if slug == "index" else slug, "title": meta["title"],
                     "group": meta.get("group", "Guide"), "desc": meta.get("desc", ""), "body": body})
    return docs


def add_anchors(body):
    toc = []

    def repl(m):
        level, inner = m.group(1), m.group(2)
        sid = slugify(inner)
        if level == "2":
            toc.append((sid, re.sub(r"<.*?>", "", inner)))
        return f'<h{level} id="{sid}"><a class="anchor" href="#{sid}">#</a>{inner}</h{level}>'
    return re.sub(r"<h([23])>(.*?)</h\1>", repl, body), toc


def build_docs(docs, search):
    groups = {}
    for d in docs:
        groups.setdefault(d["group"], []).append(d)

    for i, d in enumerate(docs):
        depth = 1 if not d["slug"] else 2
        root = "../" * depth
        side = []
        for g, items in groups.items():
            side.append(f'<div class="side-group"><div class="side-head">{g}</div>')
            for it in items:
                href = f'{root}docs/{it["slug"] + "/" if it["slug"] else ""}'
                cur = ' aria-current="page"' if it is d else ""
                side.append(f'<a href="{href}"{cur}>{it["title"]}</a>')
            side.append("</div>")
        body, toc = add_anchors(highlight(d["body"]))
        toc_html = "".join(f'<a href="#{sid}">{html.escape(t)}</a>' for sid, t in toc)
        prev = docs[i - 1] if i > 0 else None
        nxt = docs[i + 1] if i + 1 < len(docs) else None

        def link(x, cls, label):
            if not x:
                return "<span></span>"
            href = f'{root}docs/{x["slug"] + "/" if x["slug"] else ""}'
            return f'<a class="pager-{cls}" href="{href}"><small>{label}</small>{x["title"]}</a>'

        page = f"""<div class="docs">
  <button class="side-toggle" aria-label="Menu">☰ Menu</button>
  <aside class="side">{''.join(side)}</aside>
  <main class="doc">
    <div class="doc-kicker">{d["group"]}</div>
    <h1>{d["title"]}</h1>
    {f'<p class="lede">{d["desc"]}</p>' if d["desc"] else ''}
    {body}
    <nav class="pager">{link(prev, "prev", "← Previous")}{link(nxt, "next", "Next →")}</nav>
    <a class="edit" href="{REPO_URL}/edit/main/site/docs/">Edit these docs on GitHub</a>
  </main>
  <aside class="toc">{('<div class="toc-head">On this page</div>' + toc_html) if toc else ''}</aside>
</div>"""
        out = DIST / "docs" / d["slug"] / "index.html" if d["slug"] else DIST / "docs/index.html"
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(layout(f'{d["title"]} · gooseneck docs', page, desc=d["desc"] or d["title"],
                              depth=depth, page_class="page-docs"))

        url = f'docs/{d["slug"] + "/" if d["slug"] else ""}'
        search.append({"t": d["title"], "s": d["group"], "u": url,
                       "x": re.sub(r"\s+", " ", re.sub(r"<.*?>", " ", d["body"]))[:2000]})
        for sid, t in toc:
            search.append({"t": t, "s": d["title"], "u": f"{url}#{sid}", "x": ""})


# ---------------------------------------------------------------- main

def main():
    if DIST.exists():
        shutil.rmtree(DIST)
    shutil.copytree(SITE / "static", DIST)
    for gif in ("demo.gif", "cli.gif"):
        shutil.copy(REPO / gif, DIST / gif)

    pals = palettes()
    (DIST / "themes.js").write_text(
        "window.GOOSE_THEMES=" + json.dumps(pals, separators=(",", ":")) + ";\n"
        + (SITE / "themes-runtime.js").read_text())

    stats = catalog_stats()
    search = []
    docs = load_docs()
    build_docs(docs, search)

    landing = (SITE / "landing.html").read_text()
    tag_html = "".join(
        f'<li><span>{t}</span><b>{n:,}</b></li>' for t, n in sorted(stats["tags"].items(), key=lambda x: -x[1]))
    faces = "".join(
        f'<figure><img src="{html.escape(a["img"])}" alt="" loading="lazy" referrerpolicy="no-referrer" '
        f'onerror="this.parentNode.remove()"><figcaption>{html.escape(a["name"])}</figcaption></figure>'
        for a in stats["famous"])
    chips = "".join(
        f'<button class="chip" data-theme-pick="{n}" style="--c1:{p["primary"]};--c2:{p["secondary"]};--c3:{p["accent"]};--cb:{p["bg"]}">'
        f'<i></i>{n}</button>' for n, p in pals.items())
    landing = (landing.replace("{{COUNT}}", f'{stats["count"]:,}')
               .replace("{{THEMES}}", str(len(pals)))
               .replace("{{TAGS}}", tag_html)
               .replace("{{FACES}}", faces)
               .replace("{{CHIPS}}", chips)
               .replace("{{INSTALL}}", html.escape(INSTALL))
               .replace("{{REPO}}", REPO_URL))
    landing = highlight(landing)
    (DIST / "index.html").write_text(layout(
        "gooseneck — Jewish music in your terminal", landing,
        desc=f'A curated catalog of {stats["count"]:,} Jewish music artists, a beautiful terminal player and a fully scriptable CLI.',
        depth=0, page_class="page-home"))

    search.append({"t": "Install", "s": "Home", "u": "docs/install/", "x": INSTALL})
    (DIST / "search.json").write_text(json.dumps(search, separators=(",", ":")))

    (DIST / "404.html").write_text(layout("Not found · gooseneck", """<main class="notfound">
  <div class="nf-code">404</div><h1>This track isn't in the catalog.</h1>
  <p>The page you wanted doesn't exist. Maybe it's still downloading.</p>
  <p><a class="btn btn-primary" href="/">Back home</a> <a class="btn" href="/docs/">Read the docs</a></p>
</main>""", desc="Page not found", depth=0, page_class="page-404", root="/"))

    n = sum(1 for _ in DIST.rglob("*.html"))
    print(f"built {n} pages, {len(pals)} themes, {stats['count']} artists -> {DIST}")


if __name__ == "__main__":
    main()
