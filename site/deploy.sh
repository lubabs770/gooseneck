#!/bin/sh
# Build the site and publish it to Cloudflare Pages (project "gooseneck").
# Needs a logged-in wrangler (`npx wrangler login`).
set -e
cd "$(dirname "$0")"
python3 build.py
npx -y wrangler pages deploy dist --project-name gooseneck --branch main --commit-dirty=true
