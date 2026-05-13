---
title: "s08 · 插件验证与测试桩"
chapter: 08
slug: s08-verify-plugin
est_read_min: 7
---

# s08 · 插件验证与测试桩

> 这一章教什么：怎么把一个 ad-hoc 的 bash "文件齐不齐？" 脚本，升级成一个结构化、可代码化的验证 harness —— 稳定的 `Issue` code、可注入的 `Runner` 用于 smoke-test 被包装的 harness、以及一个 agent（或 CI）能直接 branch 的 JSON 信封。

## Problem

每个写 CLI-Anything 插件的人都会撞到同一堵墙：第一次往 hub 推就静悄悄地挂了 —— 要么 (a) `SKILL.md` 漏了 `description`，要么 (b) harness 在 `--json` 下崩了，要么 (c) 忘了 `README.md`。上游的答案是 `cli-anything-plugin/verify-plugin.sh` —— 56 行 bash，挨个 tick 必备文件、有差错就 exit 非零。能用，但只要一旦插件不止一个，三个问题立刻冒出来：

1. **输出没结构。** 想消费这份报告的 agent 只能字符串匹配 "MISSING"。没有稳定的 code 可以 branch。
2. **只检查文件在不在。** 上游验"形状"。它从来不问 harness "你跑得起来吗？你真的吐 JSON 信封吗？" 一个插件可以过 `verify-plugin.sh`、却完全不能用。
3. **难单测。** Bash 直接 shell-out；要测验证器自己就得在磁盘上放真 binary。我们的 Go port 想要 `Runner` 接口，让测试塞一个 fake 进去。

## Solution

一组 flat 的 `check_*` 函数，每个返回 `[]Issue`，由 `Verify(dir, runner)` 累加成 `Report{Issues, Pass}`。每个 Issue 带一个 `Severity`（`error` 或 `warn`）和稳定的 `Code`（`S001`..`S008`）。

```go
type Issue struct {
    Severity string `json:"severity"` // "error" | "warn"
    Code     string `json:"code"`     // 如 "S001"
    Message  string `json:"message"`
    Path     string `json:"path,omitempty"`
}

type Runner interface {
    Exec(ctx context.Context, args []string, stdin []byte) (exitCode int, stdout, stderr []byte, err error)
}

type Report struct {
    Plugin string  `json:"plugin"`
    Issues []Issue `json:"issues"`
    Pass   bool    `json:"pass"`
}
```

Checks 分两族：文件系统检查（S001、S002、S003、S005、S007、S008）和运行时 smoke test（S004、S006）。两者走同一个 orchestrator；`Runner` 接口是唯一的接缝。

三个值得点名的决策：

1. **不短路。** 每个能跑的 check 都跑。插件作者一次看到全部五个问题，而不是每次 push 才发现一个 —— 和上游 `ERRORS=$((ERRORS+1))` 的累加器是同一个模式。
2. **`warn` ≠ `error`。** 漏 `description` 烦人但不致命。漏 `name` 致命 —— 每个下游消费者都要它。`Pass` 是"没有 error-severity 的 issue"；warning 出现但不让整体 fail。
3. **Runner 是 interface 不是 `exec.Cmd`。** shell-out 实现住在 `main.go` 的 `shellRunner`；测试塞一个脚本化的 `FakeRunner`。和 s06 hub fetcher 用 `httptest` 是同一种 pattern。

## How It Works

```text
Verify(pluginDir, runner)
    │
    ├── stat(pluginDir)           ── 出错 → S000
    │
    ├── check_skill_md_required_fields
    │       └── findSkillMD → ParseSkill → 断言 Name 存在；Description 缺失则 warn
    │
    ├── check_skill_md_triggers
    │       └── 把 frontmatter 再 unmarshal 进 yaml.Node，断言 Sequence 形状（S005）
    │
    ├── check_readme_exists
    │       └── stat(<dir>/README.md)（S008）
    │
    ├── check_harness_has_help
    │       └── runner.Exec("<harness> --help", ...) → 断言 exit 0 + 非空（S006/S007）
    │
    └── check_harness_supports_json
            └── runner.Exec("<harness> --json", ...) → json.Unmarshal → 断言含 "ok" key（S004）

    ↓
   按 Code 排序 → Pass = (没有 error-severity 的 issue)
```

`verify.go` 里的 orchestrator：

```go
func Verify(pluginDir string, runner Runner) (*Report, error) {
    rep := &Report{Plugin: pluginDir}
    // ... stat checks ...
    rep.Issues = append(rep.Issues, check_skill_md_required_fields(pluginDir)...)
    rep.Issues = append(rep.Issues, check_skill_md_triggers(pluginDir)...)
    rep.Issues = append(rep.Issues, check_readme_exists(pluginDir)...)
    rep.Issues = append(rep.Issues, check_harness_has_help(pluginDir, runner)...)
    rep.Issues = append(rep.Issues, check_harness_supports_json(pluginDir, runner)...)
    sort.SliceStable(rep.Issues, func(i, j int) bool { return rep.Issues[i].Code < rep.Issues[j].Code })
    rep.Pass = !hasErrors(rep.Issues)
    return rep, nil
}
```

三个不太显眼的点：

1. **`Runner` 是以 interface 收的，不是 type。** 这是 `verify_test.go` 之所以便宜的关键：一个 30 行的 `FakeRunner`（基于 `map[string]canned`）就能覆盖全部五个场景，根本不用编译真插件。生产用的 `shellRunner` 也就 25 行的 `exec.CommandContext("/bin/sh", "-c", ...)`，测试从来不碰它。
2. **`check_skill_md_triggers` 重解析 frontmatter。** 我们的 `ParseSkill` 返回的 `SkillMeta` 里 `Triggers` 是 `[]string`，所以 YAML 的强制转换会悄悄把 `triggers: foo` 变成 `[]string{}`、丢掉这个 bug。于是我们再 unmarshal 一次到 `map[string]yaml.Node`，断言 `Kind == SequenceNode` —— 正是上游用 `python3 -c "import json"` 给 plugin.json 做的同一种结构检查。
3. **Issues 在返回前按 Code 排序。** `os.ReadDir` 返回的发现顺序不是确定的 —— 排序保证测试断言稳定、人类渲染器的输出在多次跑之间 diff 得动。

## What Changed（vs. s07）

s07 关心的是怎么把插件 *拿到* 磁盘上（installer 派发）。s08 关心的是 *验证* 结果。和整个 curriculum 比起来，有三处具体的 delta：

- **新顶层类型：`Report`。** s01..s07 返回的是领域数据或 error。s08 返回的是 *结构化的发现集合* —— 更像 linter 而不是 CLI。`Issue` 的 shape 就是外层 agent 用来决定"发布 vs 拦截"的输入。
- **`Runner` 接口，s08 首次出现。** 早期章节要么是进程内跑（`Dispatch`），要么直接 shell-out 没有抽象（s07 的 installer）。验证器是第一个 *两边都要* 的场景 —— 生产 shell-out、测试 fake —— 接缝在这里才划算。
- **`SKILL.md` 解析器以更弱的不变式重新声明。** s02 的 `Parse` 在 `name` 缺失时直接报错；s08 的 `ParseSkill` 照样返回 struct，好让 `check_skill_md_required_fields` 发出一个结构化的 `S002`、而不是一个 Go error。同样的输入、不同的消费者。

## Try It

```bash
cd agents/s08-verify-plugin
make demo            # 对 testdata/plugin-good 跑出人类可读报告
make demo-json       # agent 看到的 JSON 信封
make test            # 5 个 FakeRunner 驱动的场景
```

期望的 demo 输出：

```text
{
  "plugin": "./testdata/plugin-good",
  "issues": null,
  "pass": true
}
```

以及 JSON 信封：

```json
{"ok":true,"data":{"plugin":"./testdata/plugin-good","issues":null,"pass":true}}
```

试着把 fixture 弄坏 —— 删掉 `testdata/plugin-good/README.md` 再跑，看到 `S008` 触发、其余照旧绿。

## Upstream Source Reading

把 [`cli-anything-plugin/verify-plugin.sh`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/verify-plugin.sh)（56 行）和我们的 `verify.go` + `checks.go`（合起来约 300 行）对照读。重点：

- **`check_file()`（bash 第 10-17 行）** —— 发 `✓` / `✗`、累 `ERRORS` 的 helper。我们的 `Issue{Severity:"error", Code:"S00X"}` 是它的类型化等价；同一个累加器，结构更多。
- **`Required files:` 段（bash 第 19-29 行）** —— 一个硬编码的路径列表。我们的 `check_skill_md_required_fields` + `check_readme_exists` 覆盖了课程相关的子集；生产验证器还会再加 `LICENSE`、`PUBLISHING.md`、以及 `commands/*.md`。
- **`Checking plugin.json validity` 段（bash 第 31-37 行）** —— shell-out 到 `python3 -c` 做 JSON-parse 校验。我们对 YAML frontmatter（yaml.v3）和 triggers 形状（再 unmarshal 到 `yaml.Node`）做同样的事。
- **`Checking script permissions` 段（bash 第 39-46 行）** —— 检查 `setup-cli-anything.sh` 的可执行位。我们没显式做；如果 `harness` 不可执行，`Runner` 调用会返回 `127`/permission-denied，`S006` 自动 catch 住。

离线 close copy 在 [`upstream-readings/s08-verify-plugin.md`](../../upstream-readings/s08-verify-plugin.md) —— 包括原文和"保留/改动/省略"的对照。

上游还有一个 `cli-anything-plugin/tests/` 目录，里面的 pytest fixture 驱动了多插件验证 run。我们的 `verify_test.go` 沿用同样的 "每条规则一个 happy-path + 一个反例" 模式，只是 Go 版。
