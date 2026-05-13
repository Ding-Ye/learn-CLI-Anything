---
name: plugin-good
version: 0.1.0
description: A demo plugin used by `make demo` for the s10 publish pipeline.
---

# plugin-good

A trivial plugin that exists only to give the s10 publisher something
to scan, validate, bundle, sign, and index.

The real upstream equivalents are 60+ directories at the CLI-Anything
repo root: `blender/`, `audacity/`, `anygen/`, etc. Each one has a
SKILL.md at the top — that's the discovery contract `publish run` keys
off of.
