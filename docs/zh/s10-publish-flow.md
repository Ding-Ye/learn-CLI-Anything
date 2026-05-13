---
title: "s10 · 发布流：CI + 注册中心同步"
chapter: 10
slug: s10-publish-flow
est_read_min: 8
---

# s10 · 发布流：CI + 注册中心同步

> 这一章教什么：CLI-Anything 的 CI 端是怎么把一个装着插件子目录的文件夹变成一份签名好、有索引的 release。我们把 `.github/workflows/publish-cli-hub.yml` + `check-root-skills.yml` 的形状，加上 `cli-hub/cli_hub/` 的打包面，port 成一个本地运行、每次重跑都产出比特相同字节的五步流水线。

## Problem

s01-s09 搭好的是 harness 的**运行时**：解析器、REPL、注册中心、安装器、anygen 案例。但这些都回答不了一个问题：插件到底是怎么进到安装器读的那份注册中心里的？上游的答案在 CI 里：`cli-hub/**` 在 `main` 上有变更时，一个 workflow 重新打包、查 PyPI 看版本是不是已经在了、然后 publish；另一个 workflow 在 PR 时校验每个 harness 的 SKILL.md 和仓库根 `skills/` 镜像是否一致。两个脚本、两个 workflow YAML 在一次 push 上协同。我们要做的是用一个本地 Go 二进制把这套流抓出来——但不真的 push——这样课程保持 hermetic、可复现。

## Solution

一个 `Pipeline` 结构体，五个方法，按顺序跑，每一步返回一个 `StepReport`：

```text
ScanPlugins(src)   src/ 一层深的目录，收集所有带 SKILL.md 的子目录
Validate           SKILL.md 必须存在且非空
Bundle             tar.gz 每个插件目录 → out/<name>-<version>.tar.gz
Sign               sha256 每个 artifact → out/<artifact>.sha256
UpdateIndex        生成 out/registry.json，每个插件一行
```

`Run(ctx, src, out)` 串起来，把每步的 report 汇总成 `PipelineReport`。三个值得点名的设计决策：

1. **比特可复现。** 每个 `tar` header 写死 `fixedMTime = 2024-01-01 UTC`，uid/gid 清零、uname/gname 抹掉。上游通过 `python -m build` 内部的 `SOURCE_DATE_EPOCH` 拿到同样属性；我们用一个常量。同样输入跑 `publish run` 产出字节完全一致的 tarball，意味着 sha256 一致，意味着 CI 重跑不会无意义地搅动 index。
2. **`Sign` 是 digest，不是真签名。** 真正的签名（Sigstore、PyPI trusted-publishing）不在课程范围里；这里的点是告诉你签名**插在流水线哪一步**。sidecar 格式遵循 GNU `sha256sum`，现成工具就能验。
3. **Hermetic。** 不发 HTTP、不 `git push`。Publisher 写出一个目录，你可以 rsync 到 CDN 或 `gh-pages` 分支——但流水线在这里就停。上游 workflow 里的 `pypa/gh-action-pypi-publish` 步正是我们**不** port 的那一步——它会把课程绑进网络状态。

## How It Works

```text
Pipeline.Run(ctx, src, out)
   │
   ├─ ScanPlugins(src)        对每个带 SKILL.md 的子目录:
   │                            readSkillFront → Manifest{Name, Version, Backend, Entry, Skill}
   │                            （按 Name 排序，保证顺序确定）
   │
   ├─ Validate(src)           stat 每个 Manifest.Skill；缺失/空都失败
   │
   ├─ Bundle(src, out)        对每个 Manifest:
   │                            writeTarGz(src/Entry → out/<name>-<version>.tar.gz)
   │                            （固定 mtime、清零 uid/gid）
   │
   ├─ Sign(out)               对每个 artifact:
   │                            sha256File → out/<artifact>.sha256
   │
   └─ UpdateIndex(out)        读所有 sidecar，序列化 registry.json {meta, clis[]}
```

`Run` 五步主体（约 30 行，来自 `agents/s10-publish-flow/publish.go`）：

```go
func (p *Pipeline) Run(ctx context.Context, srcDir, outDir string) (PipelineReport, error) {
    rep := PipelineReport{SrcDir: srcDir, OutDir: outDir}
    step, err := p.ScanPlugins(srcDir)
    rep.Steps = append(rep.Steps, step)
    if err != nil { rep.OK = false; return rep, err }
    step, _ = p.Validate(srcDir)
    rep.Steps = append(rep.Steps, step)
    if !step.OK { rep.Plugins = p.Plugins; rep.OK = false; return rep, nil }
    step, err = p.Bundle(srcDir, outDir)
    rep.Steps = append(rep.Steps, step)
    if err != nil { rep.Plugins = p.Plugins; rep.OK = false; return rep, err }
    step, _ = p.Sign(outDir)
    rep.Steps = append(rep.Steps, step)
    if !step.OK { rep.Plugins = p.Plugins; rep.OK = false; return rep, nil }
    step, _ = p.UpdateIndex(outDir)
    rep.Steps = append(rep.Steps, step)
    rep.Plugins = p.Plugins
    rep.OK = allOK(rep.Steps)
    return rep, nil
}
```

三个不太显眼的点：

1. **先 Validate 再 Bundle，不是反过来。** 如果某个 SKILL.md 坏了，先打 tarball 再 index 时它一样会失败——但 blame 行会变成"sha256 mismatch"而不是真因"empty SKILL.md"。在 Validate 这里短路能让失败模式可读。
2. **error 返回保留给 I/O。** `Run` 只在 caller 无法恢复的情况下返非空 error（out-dir mkdir 失败、registry.json 写不出去）。单插件失败进入 `StepReport.Errors`，流水线继续跑——这样运维能一次看到全部受损范围。
3. **`ScanPlugins` 只挖一层，这是设计意图。** 一个插件就是 `src/` 下的一个目录。上游布局（`blender/`、`audacity/`、…）正好就是这个形状。递归会让 `testdata/plugin/sub/SKILL.md` 冒充插件——那是 bug 不是 feature。

## What Changed（vs. s09）

s09 是单个 harness 包裹一个远程 API；s10 是**元**层——它发布任何 harness。两处具体 delta：

- **课程计划里的 `Manifest` shape 在这一章终于被用作生产者。** s06 和 s07 暗示了它的消费侧；s10 是生产侧。JSON tag 和课程计划里的 canonical signature 一致，下游 s06 风格的注册中心读这个文件不需要任何转换。
- **可复现的 `tar.gz` writer。** s01-s09 写文件用的是 `os.WriteFile` 默认的 mtime。Release 打包要求字节稳定，于是 `writeTarGz` 把所有运行间会变的 header 字段都清零。

## Try It

```bash
cd agents/s10-publish-flow
make demo         # 用 testdata/ 跑完整流水线，JSON 信封
make status       # cat 出 registry.json
make test         # 五个测试，t.TempDir() 里的两插件 fixture
```

`make demo` 跑完，`out/` 里是：

```text
plugin-good-0.1.0.tar.gz
plugin-good-0.1.0.tar.gz.sha256
registry.json
```

`tar tzf out/plugin-good-0.1.0.tar.gz` 会列出 `plugin-good/SKILL.md`——tarball 用插件目录名作 root，正是安装器期望的布局。

## Upstream Source Reading

把这些和我们 350 行的 `publish.go` 对照读：

- [`.github/workflows/publish-cli-hub.yml`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/.github/workflows/publish-cli-hub.yml) —— 49 行的 workflow，push 到 `main` 时跑 `python -m build` + `pypa/gh-action-pypi-publish`。"check if version already published" 那段 curl 到 `pypi.org/pypi/<pkg>/<v>/json` 是上游的幂等检查；我们等价物是固定 mtime 的 tar + sha256——同样属性、不同机制。
- [`.github/workflows/check-root-skills.yml`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/.github/workflows/check-root-skills.yml) —— PR 时的校验关卡，调 `validate_root_skills.py`。我们把 discover+validate 折叠成 `ScanPlugins` + `Validate` 一对。
- [`cli-hub/cli_hub/registry.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/registry.py) —— `UpdateIndex` 产物的**消费方**。读完这个文件，我们 `indexEntry` 选哪些字段就一目了然：`name`、`version`、`backend`、`entry`、`skill` 是 `fetch_all_clis` 需要的；`artifact` + `sha256` 是 s07 风格的安装器在 install/verify 时要用到的。
- [`cli-hub/setup.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/setup.py) —— publish workflow 读版本号的 source of truth。我们的 `readSkillFront` 在插件层级扮演同样角色：front-matter 的 `version:` 是契约。

离线副本：两个 workflow YAML 前 200 行存在 [`upstream-readings/s10-publish-flow.yml`](../../upstream-readings/s10-publish-flow.yml)。
