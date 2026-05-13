---
title: "s04 · 预览包：指纹与缓存"
chapter: 04
slug: s04-preview-bundle
est_read_min: 7
---

# s04 · 预览包：指纹与缓存

> 本章要点：用一个内容寻址的 cache，把"agent 把同一个昂贵命令连续跑 20 遍"变成"跑一次，重放 19 次"——靠的只是 `(inputs, args)` 的一次 sha256。

## Problem

agent 循环里的命令调用是高度重复的。当 agent 在迭代一个 Blender 渲染、一段音频转写、一次 Pandoc 转换，或者任何"确定性但慢"的命令时，它会在探索附近参数或单纯重新校验状态时反复发同一个调用。每一次重发都在烧 wall-clock、GPU 时间或 API 配额。上游的 `cli-anything-plugin/preview_bundle.py` 用一个 **preview bundle** 解决这件事：把 inputs + 命令 hash 一下，把结果存到盘上，第二次命中就重放。思路和 Bazel 的 action cache、Nix 的 hash-of-inputs derivation 一样，只是裁剪到 agent harness 真正需要的"单 bundle"形态。

## Solution

一个 `Bundle` 是 `{Key, CreatedAt, CmdArgs, Files, Stdout, Stderr, ExitCode}`。`Key` 是 `(inputs, cmdArgs)` 的 canonical-JSON sha256。两个 `Store` 实现：`MemStore`（按插入顺序 LRU、容量有界，给测试和短命进程用）；`DiskStore`（一个 bundle 一个 JSON 文件，靠 tmp+rename 做原子写，给持久化用），落在 `~/.cache/learn-cli-anything-s04/`。`Run(ctx, cmd, inputs, store)` 是唯一入口：算 key，查 store，要么重放要么真跑——返回 `(*Bundle, cacheHit bool, error)` 三元组让调用方知道走的是哪条路。

三个关键设计：

1. **hash 内容，不 hash 路径。** 上游的 `fingerprint_file` 用 `{path, size, mtime_ns}`——快，但一次空 `touch` 就让 cache 失效，复制一份又会 miss。我们 hash 字节本身。大文件会慢，但是正确，而且一个迭代 4 KB SVG 的 agent 根本感知不到。
2. **canonical 化时显式排 map key。** Go 的 `encoding/json` 恰好会排 map key（从 1.12 开始），但一个内容 hash 不该依赖 stdlib 内部实现。所以我们先排序，再建 struct，再 marshal——三步，但 hash 是可证稳定的。
3. **非零退出也 cache。** 一个确定性失败仍然是可缓存的结果。`convert: no decode delegate` 报一次就会报第二次；多 exec 一次只为确认一遍是浪费。上游叫它 `status: "partial" / "error"` manifest，理由一样。

## How It Works

```text
Run(ctx, cmd, inputs, store)
    │
    ├── key = Fingerprint(inputs, cmd)        ── sha256(canonical-JSON)
    │
    ├── store.Get(key) ──▶ 命中？
    │       │
    │       ├── 是 ──▶ return (bundle, true, nil)        ◀── cache hit
    │       │
    │       └── 否 ──▶ mkdtemp + 写 inputs/
    │                       │
    │                       ├── exec.CommandContext(ctx, cmd)
    │                       │       ├── stdout → bundle.Stdout
    │                       │       ├── stderr → bundle.Stderr
    │                       │       └── exit  → bundle.ExitCode
    │                       │
    │                       └── store.Put(bundle)
    │                                │
    └─────────────────────────────── return (bundle, false, nil)
```

指纹函数核心（节选自 `agents/s04-preview-bundle/bundle.go`，约 15 行）：

```go
func Fingerprint(inputs map[string][]byte, cmdArgs []string) string {
    names := make([]string, 0, len(inputs))
    for k := range inputs { names = append(names, k) }
    sort.Strings(names)
    type entry struct { Name, Hash string }
    entries := make([]entry, len(names))
    for i, n := range names {
        sum := sha256.Sum256(inputs[n])
        entries[i] = entry{Name: n, Hash: hex.EncodeToString(sum[:])}
    }
    canon := struct {
        Inputs []entry  `json:"inputs"`
        Cmd    []string `json:"cmd"`
    }{Inputs: entries, Cmd: cmdArgs}
    buf, _ := json.Marshal(canon)
    sum := sha256.Sum256(buf)
    return "sha256:" + hex.EncodeToString(sum[:])
}
```

三个不显然的点：

1. **tempdir + `LEARN_S04_INPUT_DIR`，不是塞 argv。** `Run` 把 inputs 落盘给子进程用时，tempdir 路径走环境变量。要是塞进 `cmdArgs`，每次跑 cache key 就变（每次 tempdir 不一样），cache 永远 miss。
2. **tmp+rename 原子写。** `DiskStore.Put` 先写 `<key>.json.tmp` 再 rename 覆盖 `<key>.json`。同文件系统的 POSIX `rename` 是原子的——读者看到的要么是旧文件要么是新文件，绝不会看到写一半的 blob。上游 `write_json` 是同一套路。
3. **inputs 防御性复制。** `copyInputs` 在存之前把字节克隆一份。调用方 `Run` 之后改它的 `map[string][]byte`，污染不到 cache。

## What Changed (vs. s01)

s01 是裸的 CLI dispatch。s04 沿用同一套 `CLI` / `Result` 信封（在本模块里重声明——每章自包含），加了一层：`Run` handler 不**做事**，把 `(cmd, inputs)` 交给 bundle 层。从 agent 的视角看信封跟 s01 一模一样；cache 是透明的。这是关键：一个学会读 s01 JSON 的 agent，当上游 harness 接入 bundle pattern 时，自动享受到 s04 的 cache。

## Try It

```bash
cd agents/s04-preview-bundle
make demo
```

预期输出（截短）：

```text
--- first run (expect cache_hit=false) ---
{"ok":true,"data":{"cache_hit":false,"exit_code":0,"key":"sha256:...","stderr":"","stdout":"hello\n"}}
--- second run (expect cache_hit=true) ---
{"ok":true,"data":{"cache_hit":true,"exit_code":0,"key":"sha256:...","stderr":"","stdout":"hello\n"}}
--- cache dir contents ---
<sha256-hex>.json
```

两次跑 `echo hello`，args 一样，inputs 一样（都空）→ key 一样。第二次没 spawn `echo`，从 JSON blob 里读出来 stdout 直接返回。

也可以直接看一个 cached bundle：

```bash
./s04-preview-bundle --json -cache /tmp/c1 show sha256:<hex>
```

## Upstream Source Reading

读 [`cli-anything-plugin/preview_bundle.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/preview_bundle.py) 跟 `agents/s04-preview-bundle/bundle.go` + `exec.go` 对照。重点段：

- **`hash_data` / `fingerprint_data`**——同样的 canonical-JSON sha256。上游的 `_json_dumps` 用 `sort_keys=True`，是我们手动做的 Python 版。
- **`build_cache_key`**——上游的 key 包含 `(software, recipe, bundle_kind, source_fingerprint, options, harness_version, protocol_version)`。我们坍缩到 `(inputs, cmdArgs)`，因为 agent 流在 s04 这一层还不需要 versioning；s10 会回头处理。
- **`prepare_bundle` / `find_cached_manifest`**——上游遍历一棵 `manifest.json` 目录树。我们一个 bundle 一个文件、用 hex-of-sha256 当文件名：目录本身就是索引。取舍是丢了 per-bundle 元数据文件，但换来 O(1) 查询。

离线副本在 [`upstream-readings/s04-preview-bundle.py`](../../upstream-readings/s04-preview-bundle.py)。
