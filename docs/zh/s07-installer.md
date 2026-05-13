---
title: "s07 · 多后端安装器"
chapter: 07
slug: s07-installer
est_read_min: 8
---

# s07 · 多后端安装器

> 这一章教什么：怎么把一个 `Manifest` 真正"安装下来"——而且要同时支持四种完全不同的后端（`pip`、`npm`、`bundled`、`fake`），同时不让 dispatcher 跟其中任何一个耦合。做法是把差异藏在一个一方法的 `Shell` 接口和一个 `BundledInstaller` 后面，把结果统一记到一个 JSON ledger 里。

## Problem

s06 给的是注册中心 —— 一份 `Manifest` 列表，描述"有哪些 CLI"。但 manifest 只是元数据；agent 还得**真的把它装上**才能调用。上游用 `cli-hub/cli_hub/installer.py`（373 行）解决这件事 —— 按 `package_manager` 分支，调 `pip install`、`npm install -g`、`uv pip install`，或 fork 出 curl/bash 管道。分支本身是承重墙，但**这正是 agent 不该操心的事**。Agent 说"装 anygen"；installer 负责判断这到底是 Python wheel、npm tarball 还是打包档。

没有 dispatcher 的话，agent 会撞到三个具体痛点：

1. **每个后端的 argv 形状不同。** `pip` 要 `<name>==<version>`；`npm` 要 `<name>@<version>`；`uv` 中间还要塞个 `pip`。每个调用点重复这套逻辑，结果就是每个被包装的工具都重新推一遍。
2. **Bundled 档真的需要做文件 IO。** 一部分 CLI 自带 tarball（它包的是 GUI 应用插件目录）。下载、校验、解包 —— 包管理器一个都不会帮你做。
3. **状态必须可查。** "装了什么？"是 agent 每次规划前都要问的问题。没有 ledger 就只能把每个 manifest 拿到 PATH 上探一遍 —— 既慢又不可靠（PATH 上有名字 ≠ 这个版本确实装着）。

## Solution

一个 `Registry` 结构体，四种后端落到两条策略上，一个 JSON 文件。

```go
type Installer interface {
    Install(ctx context.Context, m Manifest) error
    Uninstall(ctx context.Context, name string) error
    List(ctx context.Context) ([]Manifest, error)
}
```

`Registry` 实现 `Installer`，按 `Manifest.Backend` 分派：

- **`bundled` → BundledInstaller**：下载 `manifest.url`、把 gzip tarball 解到 `InstallDir/<name>/`、拒绝路径穿越。
- **`pip` / `npm` / `uv` → ShellInstaller**：通过 `installArgs(m)` 算出 argv，交给注入的 `Shell`。
- **`fake` → 只动 ledger**：给 demo、测试、以及那种"安装动作发生在别处"的后端用（上游里 `bundled` 策略对 GUI 应用插件就是这种语义）。
- **其他 → 类型化错误**：dispatcher 大声失败，绝不悄悄 no-op。

三个值得点名的设计决策：

1. **`Shell` 是接口，不是函数。** 生产时挂 `RealShell{}`，转 `os/exec`；测试时挂 `FakeShell`，记录每一次调用。dispatcher 本身根本不 import `os/exec`。这是和上游最大的一处偏离 —— Python 那边 `subprocess.run` 是就地调的，写脚本没问题，但单测基本不可能写。
2. **`pip` / `npm` / `uv` 共享一个策略，不是三个。** 上游有九个几乎一样的 `_pip_install` / `_npm_install` / `_uv_install`。它们都在干"算 argv、调 subprocess.run、返回 (ok, msg)"。我们把它们坍缩成 `installShell`，由 `installArgs` 参数化 —— 一个对 `Backend` 的 switch 就完事了。同一份接口面，代码缩成 1/3。
3. **Ledger 是单一事实源。** 上游 `installed.json` 在 `~/.cli-hub/`；我们落在 `~/.cache/learn-cli-anything-s07/`（用 `os.UserCacheDir` 让跨平台布局自己解决）。形状是 `[]Manifest`，`MarshalIndent` 写出去，文件可 grep。`List` 读它；`Install` 按名替换或追加；`Uninstall` 按名删除。

## How It Works

```text
Install(ctx, m)
    │
    ├─ m.Backend == "bundled"  ──▶ installBundled
    │                                  │
    │                                  ├─ HTTP GET m.URL
    │                                  ├─ gzip + tar 解包 → InstallDir/<name>/
    │                                  └─ 拒绝 "../"/绝对路径
    │
    ├─ m.Backend in {pip,npm,uv} ─▶ installShell
    │                                  │
    │                                  ├─ installArgs(m) → ["pip","install","foo==1.2"]
    │                                  └─ Shell.Run(ctx, args[0], args[1:]...)
    │
    ├─ m.Backend == "fake"     ──▶ （什么都不做）
    │
    └─ 其他                    ──▶ "unknown backend %q"

    │
    └─▶ appendLedger(m)   （按名替换，MarshalIndent → installed.json）
```

分派核心（来自 `installer.go`）20 行：

```go
func (r *Registry) Install(ctx context.Context, m Manifest) error {
    if m.Name == "" {
        return errors.New("install: manifest.name is required")
    }
    switch m.Backend {
    case "bundled":
        if err := r.installBundled(ctx, m); err != nil {
            return err
        }
    case "pip", "npm", "uv":
        if err := r.installShell(ctx, m); err != nil {
            return err
        }
    case "fake":
        // 只走 ledger
    default:
        return fmt.Errorf("install: unknown backend %q (want one of: pip, npm, uv, bundled, fake)", m.Backend)
    }
    return r.appendLedger(m)
}
```

三个不太显眼的点：

1. **路径穿越在解包时拒绝。** `extractTarGz` 对每个 header name 做 `filepath.Clean`，拒绝跳出 dest 的条目（`..` 或绝对路径）。恶意 tarball 是任何"下载+解包"安装器的真实攻击面 —— 上游的 `_bundled_install` 不做这一步，因为它压根不解包（只跑 `detect_cmd`）。我们既然解，就必须做这个检查。
2. **`installArgs` 是纯函数。** `installShell` 和单测都调它。测试断言的 argv 和生产代码写出去的 argv 是同一份，所以 FakeShell 真正要验证的只是"dispatcher 确实用这组参数调了 Shell.Run"。完全不用对拼接后的命令行做字符串比较。
3. **重装是幂等的。** `appendLedger` 按名替换，所以 `install foo` 接着再 `install foo` 会把版本字段就地更新，而不是产生重复行。上游也一样（`installed[cli['name']] = ...` 是 dict-set）。

## What Changed（vs. s06）

s06 停在"注册中心能告诉你有什么"。s07 接上"现在你可以把它落到盘上"。三处具体的 delta：

- **重新声明的 `Manifest`。** 和 s06 是同一个结构体 —— Name、Version、Backend、Entry、Skill、Requires —— 多了一个只有 bundled 后端会读的可选 `URL` 字段。"不跨章 import"的规则要求我们重新声明，而不是从 s06 借。
- **新增一个 `Shell` 接口。** s06 是对 JSON 文件做纯 IO 加一次 HTTP fetch，没有任何步骤要 shell 出来。s07 是第一个 harness 真正 fork 外部进程的章节，`Shell` 注入正是让测试保持密封的关键。
- **Ledger 被 `Install`/`Uninstall` 修改，不再只读。** s06 那个 TTL 缓存按时间作废；s07 这个 ledger 按用户的显式动作作废。两者在上游里并存 —— s06 缓存"能装什么"；s07 记录"装了什么"。

## Try It

```bash
cd agents/s07-installer
make build
make test
```

要看完整真实 HTTP 流程：

```bash
make demo
```

`make demo` 起一个 `python3 -m http.server` 对着 `testdata/anygen-0.1.tar.gz`，写一份指向它的 manifest，跑 `install` → `list` → `uninstall`。五个单测覆盖同样的路径，不用 python：

```text
=== RUN   TestInstallBundled
=== RUN   TestInstallPipRecordsShellCall
=== RUN   TestListAfterTwoInstalls
=== RUN   TestUninstallBundledRemovesFromDiskAndLedger
=== RUN   TestUnknownBackendErrors
ok      learn-cli-anything/s07
```

## Upstream Source Reading

把 [`cli-hub/cli_hub/installer.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/installer.py)（373 行）和我们的 `installer.go`（约 300 行）对照读。重点：

- **`_install_strategy`**（Python 大约 85-99 行）—— 把 `package_manager` 映射到策略的规则表。我们的 Go 版本直接把它内联到 `switch` 里，因为 manifest 上已经有类型化的 `Backend` 字段（上游 manifest 可以不写 backend、靠启发式回填）。
- **`_pip_install` / `_npm_install` / `_uv_install`**（Python 大约 166-329 行）—— 三个几乎一样的函数；我们把它们坍缩成 `installShell` + `installArgs`，由一个 switch 驱动。
- **`_run_command`**（Python 大约 58-77 行）—— 自动识别 shell 元字符，在 `subprocess.run(shell=True)` 和 `shlex.split` 间切换。我们不需要 —— manifest 的 argv 是结构化的（`name`+`version` 两个字段，不是自由文本命令）；上游为了完整支持 `curl … | bash` 那种安装脚本才需要这个。
- **`_perform_action`**（Python 大约 289-301 行）—— 策略 → 动作的派发表。模式和我们的 `switch m.Backend` 一致；Python 那个 dict-of-dicts 稍微更声明式一点，干的事一样。
- **`install_cli` / `uninstall_cli` / `update_cli`**（Python 大约 316-373 行）—— 包住 `_perform_action` 并更新 `installed.json` 的顶层入口。我们的 `Registry.Install` / `Uninstall` 是同一套；`update` 我们没要 —— 它就是 `uninstall` 接 `install --force-reinstall`，课程不需要演示两次。

离线副本：前 200 行存在 [`upstream-readings/s07-installer.py`](../../upstream-readings/s07-installer.py)。
