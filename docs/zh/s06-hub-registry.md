---
title: "s06 · CLI-Hub 注册中心"
chapter: 06
slug: s06-hub-registry
est_read_min: 7
---

# s06 · CLI-Hub 注册中心

> 这一章教什么：怎么把远程注册中心建模成 JSON、用 HTTP 拉下来、带 TTL 的本地磁盘缓存、以及在缓存好的 `Index` 上跑 `hub search/list/info` —— 上游 `cli-hub/cli_hub/registry.py` 的骨架，约 250 行 Go。

## Problem

要装一个被包装的 CLI，agent 得先发现都有什么可装：名字、版本、安装后端。上游的答案是一个托管在 GitHub Pages 上的 `registry.json` —— 每次 `cli-hub install <name>` 都先读这个文件。但每次调用都打网络也不行（延迟、离线、GitHub 限流），把快照打进二进制里也不行（注册表会增长）。我们要的是"先拉一次、本地缓存、带 TTL"：日常用足够新、连着跑 `hub list && hub info <x>` 又足够快。

## Solution

三块可组合的零件：

1. **`Source` 接口** —— 任何能产出 `Index` 的东西。具体实现：`HTTPSource{URL}` 和 `FileSource{Path}`。demo 用 `FileSource` 所以可以离线跑；测试用 `httptest.Server` + `HTTPSource`。
2. **`Cache` 包装层** —— 自己也实现 `Source`，所以可以叠：`Cache{Source: HTTPSource{...}, Path: ..., TTL: 1h}`。`FetchIndex` 时先读盘上的信封，如果 `time.Since(cachedAt) < TTL` 就直接返回（不打底层 Source）。过期（或首次）就调用底层 Source 并重写缓存文件。如果网络出错但缓存还在，返回过期但能读的数据 —— 这是故意的，对应上游的 `try/except → cached_data` 兜底。
3. **`Hub` facade** —— 包住一个解码好的 `Index`，暴露 `Search(query) []Manifest`、`List() []Manifest`、`Info(name) (*Manifest, error)`。本来想用自由函数，但 facade 形态以后加个 `Reload()` 不用动调用方。

三个值得点名的设计决策：

1. **`Source` 是接口，不是函数指针。** 上游把 fetch-from-URL 和 fetch-from-file 都收在一个按 URL 参数化的函数里。Python 里行，Go 里就丢类型安全了。接口让 `Cache` 能包**任何**符合 Source 形状的东西；测试里的 `countingSource` 就是一个这种包装。
2. **`Cache` 在网络失败时返回过期数据。** 和上游一致，也是 `hub list` 该有的取舍：离线 agent 应该至少看到**上次能看到的内容**，而不是硬失败。新用户只在首次运行（连缓存都还没建）才会拿到错误。
3. **`Manifest` 跟着上游 JSON 形状走，不是 Go 风格的 struct。** `Backend` 字段（`pip | npm | bundled | uv`）直接喂给 s07 的 installer 派发。wire 格式跟上游 `registry.json` 保持 byte-compatible，将来要迁移时两侧都能读。

## How It Works

```text
buildSource ──▶ ┌────────────┐         ┌───────────────┐
                │ HTTPSource │ 或      │  FileSource   │
                └────────────┘         └───────────────┘
                       │                     │
                       └─────────┬───────────┘
                                 ▼
                          ┌────────────┐
                          │   Cache    │  （只包 HTTP）
                          │  TTL=1h    │
                          └────────────┘
                                 │
                            FetchIndex ──▶ Index{Updated, Manifests[]}
                                 │
                                 ▼
                          ┌────────────┐
                          │    Hub     │
                          └────────────┘
                              │  │  │
                  Search ─────┘  │  └───── Info(name) → *Manifest
                                List() → []Manifest
                                 │
                              Dispatch ──▶ stdout（JSON 或 pretty）
```

`Cache.FetchIndex` 的 30 行核心（来自 `agents/s06-hub-registry/registry.go`）：

```go
func (c *Cache) FetchIndex(ctx context.Context) (Index, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    cached, hasCached, _ := c.readCache()
    if hasCached && c.TTL > 0 {
        if c.now().Sub(cached.CachedAt) < c.TTL {
            return cached.Data, nil
        }
    }

    idx, err := c.Source.FetchIndex(ctx)
    if err != nil {
        if hasCached {
            return cached.Data, nil // 过期也好过硬失败
        }
        return Index{}, err
    }
    _ = c.writeCache(idx)
    return idx, nil
}
```

三个不太显眼的点：

1. **注入的时钟（`c.Now`）是 TTL 测试跑得快的原因。** 过期测试在两次调用之间把 `now` 往前推 10 分钟，而不是真的 sleep。生产里这个字段是 `nil`，默认走 `time.Now`。
2. **损坏的缓存文件被当成"没有缓存"。** `readCache` 吞掉 JSON 解码错误，报 `hasCached=false`。用户不重新拉一次根本修不了损坏的缓存，把解析错误抛出来只会无意义地卡住 `hub list`。
3. **缓存只包 HTTP，不包 File。** `main.go` 里只有 `--url` 模式才注入 `Cache`。本地文件再套层磁盘缓存等于多读一次盘，没收益 —— 文件本身就是缓存。

## What Changed（vs. s05）

s01-s05 处理的都是一个逻辑产物：一个 harness、一个 SKILL.md、一个 preview bundle。s06 是第一章 harness 需要去**发现**自己还没有路径的产物的。两处具体的 delta：

- **外部 I/O 面。** 前几章只碰 `os.Stdout`/`os.Stderr` 和（s04 的）本地缓存。s06 引入 `net/http` —— 网络抖动开始要紧的第一章。`Cache.FetchIndex` 里的 stale-fallback 就是对这个现实的负重妥协。
- **多种 `Source` 实现。** 接口是让"真拉取"和"测试假"都能插上的最小抽象。测试里数调用次数（`countingSource`）就是不用看 file mtime 也能证明缓存在工作的办法。

## Try It

```bash
cd agents/s06-hub-registry
make demo
```

样例输出（节选）：

```text
==> hub list
[
  {"name": "anygen", "version": "1.0.0", "backend": "pip", ...},
  {"name": "blender", "version": "0.2.0", "backend": "bundled", ...},
  {"name": "audacity", "version": "0.1.0", "backend": "pip", ...}
]

==> hub search blend
[{"name": "blender", ...}]

==> hub info anygen
{"name": "anygen", "version": "1.0.0", "backend": "pip", ...}
```

`make test` 跑五个测试：JSON round-trip、`httptest` HTTP 拉取、TTL 内 cache 命中、TTL 后过期再拉、search 子串匹配。

## Upstream Source Reading

把 [`cli-hub/cli_hub/registry.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/registry.py)（115 行）和我们的 Go `registry.go` 对照读。重点：

- **`_fetch_json`**（Python 大约 33-56 行）—— 缓存+拉取的核心。我们 Go 的 `Cache.FetchIndex` 是结构上的转写：读缓存 → 看 TTL → 落到拉取 → 写缓存 → 返回。
- **`fetch_all_clis`**（Python 大约 75-90 行）—— 把 harness registry 和 public registry 合并，每条加 `_source` 标。s06 不建模这种拆分（每次调用一个 Source）；在 Go 里对应物大概是一个 `MultiSource` 包两个 `Source` 并把 `Index.Manifests` 拼起来。
- **`search_clis` 和 `get_cli`**（Python 大约 93-114 行）—— 在 name/description/category 上做大小写不敏感的子串匹配。我们的 `Hub.Search` 匹配 Name 和 Backend（课程的 Manifest schema 砍掉了 Description）；其它面完全一致。
- **`CACHE_TTL = 3600`**（Python 第 13 行）—— 字面量的一小时默认。我们 `parseTTL` 默认值一致，并暴露成 CLI flag（`--ttl 1h`）以便批处理时调。

离线副本在 [`upstream-readings/s06-hub-registry.py`](../../upstream-readings/s06-hub-registry.py)。
