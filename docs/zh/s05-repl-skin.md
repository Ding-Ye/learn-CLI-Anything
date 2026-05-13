---
title: "s05 · REPL 外壳：交互式 harness"
chapter: 05
slug: s05-repl-skin
est_read_min: 7
---

# s05 · REPL 外壳：交互式 harness

> 这一章教什么：把任意 s01 风格的 `CLI` harness 包成交互式 REPL —— 行输入、`:` 前缀的元命令、运行时 `:json on|off` 切换（复用同一份 `Result` 信封）、以及 banner 显示自动探测到的 SKILL.md。

## Problem

s01 给的是一次性 CLI：agent 每调一次都重新启动一个进程。对无状态探针（`echo`、`time`）这没问题；但只要 agent 想在多轮之间**保持状态** —— 打开一个项目、设个 flag、跑一连串编辑、保存 —— 一次性进程立刻变得别扭。每行都冷启动浪费时间，而且所有状态都得序列化到磁盘。上游的解法是 `cli-anything-plugin/repl_skin.py`：所有被包装工具共享同一个 REPL 外壳。我们要做的是把它的**形状**用 Go 端口过来 —— ANSI 美术字可以不要。

## Solution

一个 `REPL` 结构体持有 harness、四个 `io.*` 流、内存 `History`、`JSONMode` flag、以及可选 `Skill`。`Run(ctx)` 是一个 `bufio.Scanner` 循环：

1. 打印 `> ` 并读一行。
2. 空行 —— 直接 continue。空输入是 no-op，不是"显示帮助"（和上游一致；连按两次回车不会糊一屏帮助）。
3. 以 `:` 开头的行 —— 走元命令派发（`:help`、`:skills`、`:json on|off`、`:history`、`:quit`）。
4. 其他情况 —— 按空白切，转发到 `Dispatch(ctx, r.Harness, argv, r.JSONMode, r.Out, r.Err)` —— 就是 s01 那个函数，原封不动。

三个值得点名的设计决策：

1. **元命令用 `:` 前缀。** 上游用裸词（`help`、`quit`），但凡被包装的工具自己定义了同名子命令就会撞车。我们用一个机械的前缀彻底回避。
2. **`Dispatch` 的 writer 类型放宽到 `io.Writer`。** s01 收的是 `*os.File`。REPL 在测试里要拿 `bytes.Buffer` 喂 Dispatch，所以放宽类型。生产路径的行为完全一致。
3. **不用 prompt_toolkit，也不用 readline。** `bufio.Scanner` 对 agent 已经足够了。上游的历史搜索、ANSI 调色板都是给人用的；LLM 不需要这些。`History []string` 留在内存里只是为了 `:history` 的内省能力。

## How It Works

```text
NewREPL(harness) ──▶ r.Run(ctx)
                       │
                       ├─ printBanner()              （读 r.Skill 或 harness Name/Help）
                       │
                       ▼
                  for sc.Scan():
                       │
                       ├─ line == ""        ──▶ continue
                       ├─ line[0] == ':'    ──▶ runMeta(line)
                       │                          ├─ :help    打印表
                       │                          ├─ :skills  列 subcommandNames(harness)
                       │                          ├─ :json    翻转 r.JSONMode
                       │                          ├─ :history dump r.History
                       │                          └─ :quit    返回 errQuit
                       │
                       └─ otherwise        ──▶ Dispatch(ctx, harness, argv, r.JSONMode, r.Out, r.Err)
```

完整循环（来自 `agents/s05-repl-skin/repl.go` 大约 30 行）：

```go
func (r *REPL) Run(ctx context.Context) error {
    r.printBanner()
    sc := bufio.NewScanner(r.In)
    sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
    for {
        fmt.Fprint(r.Out, "> ")
        if !sc.Scan() {
            break
        }
        line := strings.TrimSpace(sc.Text())
        if line == "" {
            continue
        }
        r.History = append(r.History, line)
        if strings.HasPrefix(line, ":") {
            if err := r.runMeta(line); err != nil {
                if errors.Is(err, errQuit) {
                    return nil
                }
                fmt.Fprintln(r.Err, "error:", err)
            }
            continue
        }
        argv := strings.Fields(line)
        _ = Dispatch(ctx, r.Harness, argv, r.JSONMode, r.Out, r.Err)
    }
    return sc.Err()
}
```

三个不太显眼的点：

1. **`errQuit` 是 sentinel，不是特殊返回。** 所有元命令统一用 `func(args) error` 签名。`:quit` 返回 `errQuit`；循环用 `errors.Is` 拦截后 `return nil`。其他 handler 返回真错误，循环打印后继续 —— 因为 `echo` 报错就退出整个 session 太敌对了。
2. **SKILL.md 探测是 best-effort。** `findSkill` 从 CWD 往上找 `SKILL.md`。找不到不报错 —— banner 退化成 harness 自己的 `Name`/`Help`。上游有更精细的搜索（仓库根的 `skills/<id>/SKILL.md`，然后是 packaged 路径）；对这个课程来说一次祖先扫描就够了。
3. **会话中途 `:json on` 等同于启动时 `--json`。** 同一个 `Dispatch` 写出同一份 `Result` 信封。Agent 可以中途切模式而不用重新挂进程 —— 这正是 stateful 工作流里 REPL 胜过一次性 CLI 的全部理由。

## What Changed（vs. s04）

s01-s04 都假设一次性生命周期：argv 进、字节出、进程退。s05 引入**会话** —— harness 常驻、状态在内存、agent 一行一行地说话。两处具体的 delta：

- **`Dispatch` 的 writer 类型** 从 `*os.File` 放宽到 `io.Writer`，让 REPL 能在测试里用 `bytes.Buffer` 驱动。对 `os.Stdout` 调用者，s01 行为不变。
- **重新声明了一个 `Skill` 解析器**（s02 的最小亲戚）。REPL banner 只用 `Name` 和 `Description` 两个字段，于是手写一个两字段 YAML reader，不引入 `gopkg.in/yaml.v3`。`go.mod` 保持零依赖。

## Try It

```bash
cd agents/s05-repl-skin
make build
./s05-repl-skin
```

进入 REPL 后：

```text
> :help
> :skills
> echo hi
hi
> :json on
json mode: on
> echo hi
{"ok":true,"data":"hi"}
> :history
> :quit
bye
```

跑 `make test` 看五个脚本式 stdin 测试（`strings.Reader` 进，`bytes.Buffer` 出）。

## Upstream Source Reading

把 [`cli-anything-plugin/repl_skin.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/repl_skin.py)（567 行）和我们 200 行的 `repl.go` 对照读。重点：

- **`print_banner`**（Python 大约 167-218 行）—— ANSI 美术字的 box，带 skill 安装提示。我们的 Go 端口把 banner 留成纯 ASCII，这样通过 pipe 转发时不会被 escape code 干扰。
- **`prompt`** + **`prompt_tokens`**（Python 大约 220-310 行）—— 生成 prompt_toolkit token 流。我们都不要：agent 不需要彩色提示符，`> ` 就够了。
- **`success`/`error`/`warning`/`info`**（Python 大约 342-358 行）—— 给人看的消息级 helper。我们没要 —— Dispatch 已经在打印；如果你要给人用的 `cli-anything-<x>`，把这几个 port 过来即可。
- **`create_prompt_session`**（Python 大约 485-510 行）—— prompt_toolkit `PromptSession` 工厂。Go 对应物大致是带补全 hook 的 `bufio.Reader`；我们把它留给具体工具自行决定。

离线副本：前 250 行存在 [`upstream-readings/s05-repl-skin.py`](../../upstream-readings/s05-repl-skin.py)。
