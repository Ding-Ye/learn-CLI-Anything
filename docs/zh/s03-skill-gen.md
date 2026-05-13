---
title: "s03 · 从 CLI 自动生成 SKILL.md"
chapter: 03
slug: s03-skill-gen
est_read_min: 8
---

# s03 · 从 CLI 自动生成 SKILL.md

> 本章要点：怎么遍历 CLI 树（s01 的结构）合成出 SKILL.md（s02 的格式）——frontmatter、synopsis、子命令表、flag 表、用法示例——全部来自 harness 作者已经声明好的数据。

## Problem

s02 给了我们能解析 / 渲染的 `Skill`（frontmatter + body）。可一开始这个 SKILL.md 是谁写的？上游 CLI-Anything 里每个被包装的 GUI 工具都自带一份手写 skill，但它们 90% 长得都一样——同样的 H1、同样的子命令表、同样的 triggers。这是 harness 作者不该重复做的体力活，更糟的是手写文件只要有人加个子命令立马就漂移。

上游用 `cli-anything-plugin/skill_generator.py` 解决：用正则扫描 CLI 源码里的 `@click.group(...)` / `@click.command(...)` 装饰器、抽取 docstring、灌进 SKILL.md 模板。Python 里这套能跑是因为元数据本来就以装饰器形式留在源码层。s01 里我们故意走了一条不同的路——`CLI` 是结构体字面量，不是装饰器链——所以 generator 退化成纯粹的树遍历，零 AST 工作。

## Solution

`GenerateSkill(cli *CLI) Skill` 做三件事：

1. **从树的叶子构造 frontmatter。** `meta.Name = cli.Name`，`meta.Description = firstSentence(cli.Help)`，`meta.Triggers` 是 `<sub>` 和 `<sub> <root>` 两种形式排序去重后的列表。两种形式都重要：搞模糊关键字匹配的 agent skill 加载器要"echo"和"echo demo"分别能命中不同 prompt 形态。
2. **合成 Markdown body。** H1 = name，接着 Synopsis、Subcommands 表、根 Flags 表（如果有）、Usage 段——每个子命令一个 `### subcommand` 子段，里头有子命令 help、自己的 flags 表（按 flag 名排）、还有能直接复制的 `bash` 示例（必填 flag 用类型占位填好）。
3. **所有东西排序。** 子命令遍历过 `sortedKeys`，flag 表用 `sort.Slice`，triggers 去重后过 `sort.Strings`。Go map 迭代是随机的，没有这些排序两次生成的字节流就不一样——这对 diff / 对缓存渲染好的 SKILL.md 都是致命的。

到 s02 的衔接就很轻：`RenderSkill(GenerateSkill(cli))` 是一条完整流水线。

## How It Works

```text
*CLI ──▶ GenerateSkill
            │
            ├─▶ firstSentence(Help) ──▶ Meta.Description
            ├─▶ deriveTriggers       ──▶ Meta.Triggers（排序、去重）
            └─▶ synthesizeBody       ──▶ Body
                    │
                    ├─ # name / help
                    ├─ ## Synopsis
                    ├─ ## Subcommands（排序的表）
                    ├─ ## Flags（根级，排序）
                    └─ ## Usage
                           └─ 每个子命令一节:
                                ### name sub / help / flags 表 / bash 示例
            │
            ▼
        Skill{Meta, Body}
```

三个不显然的点：

1. **`firstSentence` 是触发词启发式。** SKILL.md 的 `description` 字段是 agent 读了之后决定要不要加载这个 skill 的依据——必须一行能装下。我们在第一个 `.`、`!`、`?` 或换行处截断。上游 Python 是切前 100 字节，但 Go 字符串是 UTF-8，定长字节切容易把 rune 切两半，所以我们按句号边界切。
2. **必填 flag 的占位符跟类型挂钩。** 一个 `Required: true`、类型是 `string` 的 flag，bash 示例里渲染成 `--name <string>`。agent 的 planner 看到就知道这 flag 不能省——再配合 Required 列，agent 拿到两份冗余信号，这是故意的。
3. **空段不渲染。** 一个空 CLI（没子命令、没 flag）只会渲染出 H1 + help + Synopsis。empty-CLI 测试钉死这个行为；上游也一样——`command_groups=[]` 时整个 Commands 段直接消失。

`GenerateSkill` 的核心小到可以直接贴：

```go
func GenerateSkill(cli *CLI) Skill {
    meta := SkillMeta{
        Name:        cli.Name,
        Description: firstSentence(cli.Help),
        Triggers:    deriveTriggers(cli),
    }
    body := synthesizeBody(cli)
    return Skill{Meta: meta, Body: body}
}
```

其余都是渲染细节。body builder 是约 80 行 `fmt.Fprintf` 往 `strings.Builder` 上写——这种代码不需要抽象，只要排序遍历 + 稳定格式化。

## What Changed (vs. s02)

s02 给了 `ParseSkill`/`RenderSkill` 跟 `Skill` 模型——但 `Skill` 值还得有人给。s03 让任何已经按 s01 形态写好的 harness 自动拿到 `Skill`。我们加了约 200 行 `generator.go`，重新声明了 s01 的 `CLI`/`Flag` 和 s02 的 `Skill`/`SkillMeta`（无跨模块 import——规则约束），最后得到一行的流水线：`RenderSkill(GenerateSkill(cli))`。

跟上游的取舍：Python generator 能扫任何 Click CLI，连作者从没考虑过被生成的也能扫。我们的 generator 需要 harness 把元数据暴露成 `CLI` 结构。换来的是零反射、字节稳定输出、还有 `go vet` 编译期就能验元数据存在。

## Try It

```bash
cd agents/s03-skill-gen

make test    # 5 个测试：round-trip、triggers、flags 表、空 CLI、确定性
make demo    # 打印内置 demo harness 的合成 SKILL.md
```

`make demo` 的预期开头：

```yaml
---
name: demo
description: 'Demo harness: time + echo subcommands.'
triggers:
  - echo
  - echo demo
  - time
  - time demo
---
# demo
...
```

`make demo > SKILL.md` 就能拿到一份 s02 parser 能逐字节接受的文件。

## Upstream Source Reading

读 [`cli-anything-plugin/skill_generator.py:1-200`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/skill_generator.py) 跟 `agents/s03-skill-gen/generator.go` 对照。能直接 mapping 的几段：

- `extract_cli_metadata` ↔ `GenerateSkill`——同样的角色：harness → `SkillMetadata` / `Skill`。
- `extract_intro_from_readme` ↔ `firstSentence`——两个都是把 help 文本截成单行的触发摘要。
- `extract_commands_from_cli`（对 `@click.group` / `@click.command` 跑正则）↔ `synthesizeBody` 里 `sortedKeys` 对 `cli.Subcommands` 的遍历。Python 那边得处理装饰器堆叠和多行 docstring；Go 这边完全绕开，因为元数据本来就是结构化的。
- `CommandGroup` / `CommandInfo` dataclass ↔ Go `*CLI` 树里的匿名结构。

离线副本在 [`upstream-readings/s03-skill-gen.py`](../../upstream-readings/s03-skill-gen.py)。
