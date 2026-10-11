#!/usr/bin/env python3
"""Adds the shared-trip page to the API's nginx site, in place.

    sudo python3 scripts/tools/add-share-location.py <site file> <api domain> <page dir>

Called by scripts/deploy/share-page.sh. The site may carry certbot's TLS lines,
so it is edited rather than written again: in every server block of the API
domain that proxies to the gateway ("location / {"), the page's location goes
just before it. A site that has it already is left as it is.
"""
import pathlib
import sys

MARK = "ride-share-page"


def block(page_dir, indent):
    lines = [
        f"# {MARK}: the page a shared-trip link opens (scripts/deploy/share-page.sh).",
        "location = /t/ {",
        f"    root {page_dir};",
        "    try_files /index.html =404;",
        '    add_header Cache-Control "no-cache" always;',
        '    add_header Referrer-Policy "no-referrer" always;',
        '    add_header X-Robots-Tag "noindex" always;',
        '    add_header X-Content-Type-Options "nosniff" always;',
        "}",
        "",
    ]
    return "".join(indent + line + "\n" if line else "\n" for line in lines)


def server_blocks(text):
    """(start, end) of each top-level server { ... } block."""
    blocks, depth, start, i = [], 0, None, 0
    while i < len(text):
        if text.startswith("server", i) and depth == 0 and text[i + 6:].lstrip().startswith("{"):
            start = i
        ch = text[i]
        if ch == "#":
            i = text.find("\n", i)
            if i < 0:
                break
            continue
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0 and start is not None:
                blocks.append((start, i + 1))
                start = None
        i += 1
    return blocks


def main():
    site, domain, page_dir = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
    text = site.read_text()
    if MARK in text:
        print(f"{site} has the shared-trip page already")
        return

    added = 0
    for start, end in reversed(server_blocks(text)):
        body = text[start:end]
        names = [line.split(None, 1)[1].rstrip(";").split() for line in body.splitlines()
                 if line.strip().startswith("server_name")]
        if not any(domain in n for n in names):
            continue
        at = body.find("location / {")
        if at < 0:
            continue
        line_start = body.rfind("\n", 0, at) + 1
        indent = body[line_start:at]
        body = body[:line_start] + block(page_dir, indent) + body[line_start:]
        text = text[:start] + body + text[end:]
        added += 1

    if not added:
        sys.exit(f"no server block of {domain} with 'location / {{' in {site}")
    site.write_text(text)
    print(f"added the shared-trip page to {added} server block(s) of {domain}")


if __name__ == "__main__":
    main()
