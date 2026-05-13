---
title: "Appendix A · Why CLIs are right for agents"
slug: appendix-a-agent-native-thesis
---

# Appendix A · Why CLIs are right for agents

The thesis CLI-Anything builds on: agents should consume software through CLIs, not GUIs and not SDKs. This appendix walks the why.

## Today's software serves humans; tomorrow's users are agents

The upstream README's tagline. GUIs encode affordances for humans — buttons placed where eyes look, dialogs designed to be dismissed, undo buffers tuned to human reaction time. SDKs encode affordances for compilers — type-checked, version-locked, language-bound. Neither fits an LLM: a model has no eye-tracking, no compile step, and no concept of "version lock" beyond the prompt it was given.

A CLI sits exactly in the middle. It's text in, text out, with a discoverable subcommand tree and a JSON envelope on demand. Models read text natively. CLIs run anywhere a model can spawn a subprocess. The interface contract is the help string + the SKILL.md.

## Why not just call SDKs?

Three reasons:

1. **Language lock-in.** An OpenCV Python SDK is unreachable from a Rust agent without porting. A `cv2 ...` CLI is reachable from anywhere with a shell.
2. **Versioning is a CLI concern.** `imagemagick --version` is one line; `pip show pillow | grep Version` is three. Agents query CLIs constantly to verify they're talking to the right thing.
3. **Streaming and stdout discipline.** SDKs return objects; CLIs return bytes. An agent that wants to stream a 4 GB rendered video out of Blender can just `tail -f` the stdout. With an SDK, the agent has to learn the SDK's streaming idiom (which is different in every SDK).

## Why not just use APIs?

APIs are great when the software has one. But the long tail — Blender, FreeCAD, GIMP, Kdenlive, Audacity — does not have one. Wrapping their Python scripting interfaces or D-Bus surfaces in a uniform CLI is the work of CLI-Anything's `cli-anything-plugin/` framework. Once wrapped, the agent doesn't know whether the underlying tool is a CLI, an SDK, or a desktop GUI — it just runs subprocesses.

## The SKILL.md contract

The contract has three layers:

- **Front-matter** (YAML): `name`, `description`, `triggers[]`. This is what an agent runner reads to decide *whether* to invoke the harness for a given user prompt.
- **Body** (Markdown): explains what the harness does, when to use it, and example invocations. This is what the agent reads after deciding to use it.
- **Underlying CLI**: the actual executable the body teaches the agent to run.

Three layers, two parsers (YAML + Markdown), one executable. That's the entire contract.

## What CLIs let agents do that other interfaces don't

- **Compose with the shell.** `cli-hub install $(figure-out-name) && figure-out-name run scene.blend` is a one-liner. Try writing the SDK equivalent.
- **Cache results.** Content-addressed caching (s04) is trivial when input is "the argv + the file contents." With an SDK it requires opt-in instrumentation.
- **Sandboxes are easy.** Spawn the harness in a `firejail` / `bwrap` / Docker container. SDK calls have no syscall boundary.
- **Diff outputs.** Two harness invocations with different args produce two stdout dumps. Diff them — you have a build comparator. SDK objects are not diff-friendly.

## What the spec deliberately omits

- **GUI integration.** Out of scope: CLI-Anything's job is to make existing GUI tools agent-native by giving them a CLI, not to invent new GUIs.
- **Long-running orchestration.** A harness is one-shot. If you want a server, write one — and then write a harness that talks to it.
- **Auth and identity.** Delegated to whatever spawns the subprocess.

## Further reading

The HARNESS contract: [HARNESS.md](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/HARNESS.md).
