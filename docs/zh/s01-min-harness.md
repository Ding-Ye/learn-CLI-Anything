---
title: "s01 · 最小 harness：CLI + JSON 输出"
chapter: 01
slug: s01-min-harness
est_read_min: 6
---

# s01 · 最小 harness：CLI + JSON 输出

> 本章要点：能跑的最小 Go 程序，满足 CLI-Anything 的 HARNESS 契约——子命令树、`--json` 开关、`Result{OK, Data, Error}` 信封。

## Problem

CLI-Anything 的核心观点：LLM agent 用统一的 CLI 比用各种 SDK / GUI 好得多。在我们能展示框架怎么**生成** CLI（s02-s05）或**分发** CLI（s06-s07）之前，先得搞清楚一个"合格 harness"长什么样。上游的 `HARNESS.md` 是单页规范；本章给它最小的 Go 实现。

## Solution

harness 就是一个 `CLI` 结构：name、help、可选 flags、要么有子命令、要么有 `Run` 函数。`Dispatch` 用 argv 沿这棵树走下去。`--json` flag 在最外层解析（不让子命令看到），切换打印器：人类文本 vs `Result{OK, Data, Error}` 信封。三个关键设计：

1. **数据优先，不是装饰器优先。** 上游用 Click `@click.command` 装饰器，Python 里好使，但元数据 import 时绑定 + 反射解，在 Go 里我们用结构体字面量，让 s03 的 skill generator 不用 runtime tricks 就能内省。
2. **JSON 模式是纯打印，不是 handler。** handler 只返回 `(any, error)`。dispatcher 看 `--json` 决定怎么打。子命令代码里不掺杂展示逻辑。
3. **即使没数据也有 `Result.OK`。** 解析 JSON 信封的 agent 想要一个固定字段判断成功失败，而不是"靠 `error` 字段是否缺失推断"。

## How It Works

```text
argv ──▶ hasJSONFlag ──▶ Dispatch(root, argv, jsonMode, out, err)
                            │
                            ▼
                       沿 root.Subcommands 走到 argv 走完或无匹配
                            │
                            ▼
                       cur.Run(ctx, remaining) ──▶ (any, error)
                            │
                            ▼
                  jsonMode ? Result{} 信封到 stdout : 人类可读文本
```

核心 dispatcher（节选自 `agents/s01-min-harness/cli.go`，约 40 行）：

```go
func Dispatch(ctx context.Context, root *CLI, argv []string, jsonMode bool, out, errOut *os.File) int {
    cur := root
    i := 0
    for i < len(argv) {
        next, ok := cur.Subcommands[argv[i]]
        if !ok { break }
        cur = next
        i++
    }
    if cur.Run == nil {
        printHelp(cur, out, jsonMode)
        return 0
    }
    out1, err := cur.Run(ctx, argv[i:])
    if jsonMode {
        env := Result{OK: err == nil, Data: out1}
        if err != nil { env.Error = err.Error() }
        _ = json.NewEncoder(out).Encode(env)
    } else if err != nil {
        fmt.Fprintln(errOut, "error:", err)
        return 1
    } else {
        fmt.Fprintln(out, prettyPrint(out1))
    }
    if err != nil { return 1 }
    return 0
}
```

三个不显然的点：

1. **没用全局 flag 库。** stdlib `flag` 把父子命令的 flag 混在一起；我们在 dispatch 后手工解析，每个子命令拥有自己的 flag 名字空间——这也是上游所有 harness 最终都走到的形态。
2. **`printHelp` 也尊重 `--json`。** agent 跑 `demo --json`（没子命令）需要拿到机器可读的 harness 表面描述——形状跟 MCP 里 tools/list 返回的数据一样。s03 会用到这点。
3. **错误要么走 stderr（人类），要么进 `Result.Error`（JSON），从不同时两条路。** 混着 emit 是新手陷阱：tailing stdout 的 agent 会以为是漏了响应。

## What Changed (vs. （无）)

引导章，前面没有 sNN。基准是上游 `cli-anything-plugin/HARNESS.md`——我们写了约 150 行 Go 实现它的 `--json` + 子命令 + help 契约。

## Try It

```bash
cd agents/s01-min-harness

# 人类模式
make demo

# agent 看到的 JSON 信封
make demo-json
```

预期 `make demo-json` 的输出：

```json
{"ok":true,"data":"hi"}
{"ok":true,"data":{"flags":null,"help":"...","name":"demo","subcommands":["echo","time"]}}
{"ok":true,"data":{"unix":...}}
```

## Upstream Source Reading

读 [`cli-anything-plugin/HARNESS.md`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/HARNESS.md) 跟 `agents/s01-min-harness/cli.go` 对照。重点段：

- **子命令树**——什么算"好"的子命令粒度（一个 verb 一条命令）。
- **输出模式**——JSON 信封的形状（`ok`、`data`、`error`）固定，harness 不能自创字段。
- **退出码**——错误非零，成功 0；JSON 模式也写 `ok:false` 这样 agent 不用解析 shell wrapper 的退出码就能分支。

离线副本在 [`upstream-readings/s01-min-harness.md`](../../upstream-readings/s01-min-harness.md)。
