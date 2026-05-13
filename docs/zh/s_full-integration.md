---
title: "s_full · 端到端集成穿刺"
chapter: full
slug: s_full-integration
est_read_min: 9
---

# s_full · 端到端集成穿刺

> 本章要点：十章如何拼成一个用户流程——agent 让 hub 装一个 harness，跑它，拿到能解析的 JSON。

## Problem

我们写了十个独立的 Go module，每个都是一个孤岛。课程的教学价值要等你能把一次用户请求顺着每一层穿到底、看清每章为什么存在时，才真正显形。没有这条线，s04 的预览缓存和 s08 的 verify 像是松散的尾巴；有这条线，它们是承重梁。

## Solution

挑一个标准用例走一遍：*"agent 用 hub 装 `anygen`，然后让它从一段 prompt 生成一份 SKILL.md。"* 路上经过五章（s06 装机时的注册中心查询、s07 安装、s09 anygen 客户端、s02 SKILL.md 解析、s01 dispatch）。剩下五章（s03 skill-gen、s04 preview、s05 REPL、s08 verify、s10 publish）站在路边一步之遥；标注它们插入到哪。

先看图，再看 16 步。

```text
┌────────┐  hub install   ┌────────┐ download  ┌────────┐
│ agent  │ ─────────────► │  s06   │ ────────► │  s07   │
│        │                │ regist │           │ instal │
│        │ ◄───────────── │  ry    │ ◄──────── │  ler   │
└───┬────┘  manifest      └────────┘  receipt  └───┬────┘
    │                                              │
    │  anygen submit "make a SKILL.md for X"       │
    │   ─────────────► ┌────────┐ HTTP POST  ┌────┴───┐
    │                  │  s01   │ ────────► │  s09   │
    │                  │ dispat │           │ anygen │
    │  ◄────────────── │  ch    │ ◄──────── │ client │
    │   {"ok":true,    └────────┘           └────────┘
    │    "data":{...}}                          │
    │                                           │  result
    │  parse SKILL.md ─────────────────► ┌──────┴─────┐
    │                                    │   s02      │
    │  ◄──────────────────────────────── │ skill-md   │
    │   Skill{Meta, Body}                └────────────┘
```

## The 16-step execution trace

| 步骤 | 动作 | 章节 | 文件 / 函数 |
|-----:|------|------|--------------|
|  1 | Agent 读自己的计划，决定要用 `anygen` skill | （路外） | — |
|  2 | Agent 跑 `hub install anygen` | s06 + s07 | `s06/main.go`, `s07/main.go` |
|  3 | Hub HTTP 拉 index（缓存未命中 → 真 GET） | s06 | `s06/registry.go: HTTPSource.FetchIndex` |
|  4 | Cache 存 index，TTL=24h | s06 | `s06/registry.go: Cache.FetchIndex` |
|  5 | Hub 查 `anygen` → 返回 Manifest{Backend:"bundled", URL:...} | s06 | `s06/commands.go: Hub.Info` |
|  6 | Installer 按 Backend=bundled 分派到 BundledInstaller | s07 | `s07/installer.go: Registry.Install` |
|  7 | BundledInstaller GET 拿 tarball，解到安装目录 | s07 | `s07/installer.go: extractTarGz` |
|  8 | Installer 把 manifest 追到 `installed.json` ledger | s07 | `s07/installer.go: appendLedger` |
|  9 | Agent 跑 `anygen submit "make a SKILL.md for X"` | s01 + s09 | `s01/cli.go: Dispatch` |
| 10 | Dispatch 沿 argv 找到 `submit` 子命令 | s01 | `s01/cli.go: Dispatch` |
| 11 | submit handler 调 APIClient.SubmitJob（POST /jobs） | s09 | `s09/client.go: SubmitJob` |
| 12 | 远端服务排队，返回 `{"jobID":"abc"}` | s09 | （HTTP） |
| 13 | Agent 跑 `anygen wait abc` → 轮询循环 | s09 | `s09/poller.go: WaitForResult` |
| 14 | 若干次后，服务返回 Status:"succeeded" + Result | s09 | `s09/client.go: FetchResult` |
| 15 | Dispatch 把 `JobResult` 包成 `Result{OK:true, Data:...}` | s01 | `s01/cli.go: Dispatch` (JSON 分支) |
| 16 | Agent 解析 `Result.Data` → 装载 SKILL.md → 下一步计划 | s02 | `s02/skill.go: Parse` |

路外章节怎么接入：

- **s03 skill-gen**：agent 自己想造一个新 harness 时上场。第 16 步之后，agent 可以对自己的 `CLI` 结构体跑 `skill-gen` 输出 SKILL.md 供安装。
- **s04 preview-bundle**：包住第 9-15 步做内容寻址缓存；同样 prompt 第二次提交直接回放，不再打远端。
- **s05 repl-skin**：第 9-15 步的人类外壳。同一套 dispatch、同一份 SKILL.md，从 `> ` 提示符驱动。
- **s08 verify-plugin**：第 7 和第 8 步之间的守门人——installer 在记账前先校验 bundle 结构。
- **s10 publish-flow**：第 3 步之前的上游——维护者每发布一个新插件，registry index 都由 s10 的 pipeline 重新生成。

## Deliberate omissions

- **Auth。** 没有 bearer token、签名 manifest、按 capability 隔离的安装权限。上游同样松，要紧起来是真实世界的练习。
- **并发安装。** s07 的 ledger 没有 fcntl 锁。上游用 `fcntl.flock`；Go port 可以用 `golang.org/x/sys/unix.Flock` 或 `os.OpenFile(... O_EXCL)`。范围外。
- **真后端。** s07 通过 FakeShell 桩掉 `pip install`，从未真调 pip。上游真的 shell out，那就是 `RealShell.Run` 里 10 行代码的差。
- **遥测。** 没有调用计数、没有版本更新推送。上游 hub 前端两者都有。
- **多租户缓存。** s04 预览缓存是单用户的。上游按 harness 版本隔离；我们压缩到 args-only 主键。
- **SSE / 流式。** s09 anygen 是轮询的。上游同时支持 poll 和 SSE，我们只接了 poll。

## Try It

每个模块自带 demo；下面手工把其中三个串起来：

```bash
# 第 2-8 步：安装
cd agents/s06-hub-registry && make demo
cd agents/s07-installer    && make demo

# 第 9-15 步：提交 + 等待
cd agents/s09-anygen-remote && make demo

# 第 16 步：解析得到的 SKILL.md
cd agents/s02-skill-md && make demo
```

要一段单进程内断言每一步的真 Go 测试，看 `agents/s_full-integration/trace_test.go`（留作练习——所有积木都备齐了；缺一个 fixture registry JSON + 一个 fake anygen 的 httptest server）。

## Upstream Source Reading

整条 trace 跟上游自己的端到端流程一一对应，每一步只是把语言换了：

- Hub 注册中心：[`cli-hub/cli_hub/registry.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/registry.py)
- 安装器：[`cli-hub/cli_hub/installer.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/installer.py)
- anygen 入口：[`anygen/agent-harness/ANYGEN.md`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/anygen/agent-harness/ANYGEN.md)
- SKILL.md 解析：[`cli-anything-plugin/skill_generator.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/skill_generator.py)
