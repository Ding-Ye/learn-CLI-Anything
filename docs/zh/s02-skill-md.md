---
title: "s02 · SKILL.md 解析与渲染"
chapter: 02
slug: s02-skill-md
est_read_min: 7
---

# s02 · SKILL.md 解析与渲染

> 本章要点：一个能字节级 round-trip 的 YAML frontmatter + Markdown 解析器，针对的是 `SKILL.md`——agent 知道某个 CLI-Anything harness 存在、做什么、靠哪些词触发，全靠这一个文件。

## Problem

CLI-Anything 里每个 harness 都带一份 `SKILL.md`。上游 `cli-anything-plugin/skill_generator.py` 负责写，下游 agent（Claude Code、Pi、Codex）负责读。格式看着简单——三横线包住一段 YAML frontmatter，下面跟一坨不透明 markdown——但字节是真较真的。Generator 会用折叠标量 `>-` 把长 `description` 折行，如果解析器走 `yaml.Marshal` 重新 emit，会把折叠标量悄悄改回普通字符串，diff 一比，可重现构建的 CI 直接炸。所以解析器有俩活：抽出结构化字段，同时把原始 frontmatter 字节留着，让 `Parse → Render` 真正幂等。

## Solution

两个类型，两个函数。`SkillMeta`（frontmatter 的结构化视图）是带 `yaml` tag 的普通 Go struct。`Skill` 把它和 body 字节包起来，再加一个未导出的 `raw []byte` 存原始 frontmatter 切片。`Parse(data []byte) (Skill, error)` 找两个 `---\n` 围栏，把中间字节快照到 `raw`，再 `yaml.Unmarshal` 一份*副本*到 `SkillMeta`。`Render(s Skill) []byte` emit `---\n` +（`s.raw` 有就用，没有就 `yaml.Marshal(s.Meta)`）+ `---\n` + body。raw-bytes 快路径保证 round-trip；marshal 路径给新构造的 skill 用（s03 就会造）。校验有意做得很轻——只 require `name`，对齐上游 `_canonical_skill_name` 的不变式。

## How It Works

```text
bytes ─▶ Parse ─┬─▶ 找第一个 '---\n'  (offset 0；否则 errMissingDelim)
                ├─▶ 找第二个 '---\n'  (否则 errMissingDelim)
                ├─▶ 快照 raw = 两围栏之间的字节
                ├─▶ yaml.Unmarshal(raw) → SkillMeta
                ├─▶ 要求 Meta.Name != ""  (否则 errMissingName)
                └─▶ Skill{Meta, Body, raw}

Skill ─▶ Render ─┬─▶ "---\n"
                 ├─▶ raw (len>0) 否则 yaml.Marshal(Meta)
                 ├─▶ "---\n"
                 └─▶ Body
```

`agents/s02-skill-md/skill.go` 里关键的 30 行：

```go
func Parse(data []byte) (Skill, error) {
    if !bytes.HasPrefix(data, delim) { return Skill{}, errMissingDelim }
    rest := data[len(delim):]
    end  := bytes.Index(rest, delim)
    if end < 0 { return Skill{}, errMissingDelim }
    frontRaw := rest[:end]
    body     := rest[end+len(delim):]
    var meta SkillMeta
    if err := yaml.Unmarshal(frontRaw, &meta); err != nil {
        return Skill{}, fmt.Errorf("skill: yaml: %w", err)
    }
    if meta.Name == "" { return Skill{}, errMissingName }
    return Skill{Meta: meta, Body: body, raw: frontRaw}, nil
}
```

三个不显然的点：

1. **`raw` 是 round-trip 的关键。** 没它，第三个测试（折叠标量保形）就挂——yaml.v3 会把 `name: >-\n  cli-anything-anygen\n` 归一化成 `name: cli-anything-anygen\n`，字节散了，下游 diff 炸了，CI 炸了。原样存着，只在调用方从零造 `Skill` 时才重新 emit。
2. **typed error，不光是字符串。** `errMissingDelim` 和 `errMissingName` 是哨兵值，调用方（s03 generator、s08 validator）能 `errors.Is` 来分支。测试集就靠这个对上游 `verify-plugin.sh` 关心的两种失败模式做断言。
3. **Body 是 `[]byte`，不是 `string`。** SKILL.md 的 code block 里可能塞非 UTF-8 片段（二进制协议示例，`anygen` 文档里的 hex dump）。转成 `string` 今天没事，但堵死了将来 binary-aware 的用法；用 `[]byte` 不亏。

## What Changed (vs. s01)

s01 给了我们 `Dispatch` 和 JSON 信封。s02 复用这两个（再声明一遍，因为每个 session 都是独立 Go module——见 `cli.go`），新加一个纯模块：`skill.go`。CLI 表面多了 `parse` 和 `render` 子命令；用不到 s01 没有过的东西。唯一新依赖：`gopkg.in/yaml.v3`。净增：~120 行 Go，5 个测试，一个 demo target 临时塞到 `/tmp` 的 Markdown 样例。

## Try It

```bash
cd agents/s02-skill-md

# 解析一份示例 SKILL.md（Makefile 自动生成）
make demo

# 对真实上游 skill 跑 round-trip
go test -run TestRoundTripPreservesBytes -v
```

预期 `make demo` 的 JSON 信封（节选）：

```json
{"ok":true,"data":{"body":"# demo\n\nhello world\n",
                   "meta":{"name":"cli-anything-demo",
                           "description":"tiny demo skill",
                           "triggers":["demo","hello"]}}}
```

## Upstream Source Reading

读 [`cli-anything-plugin/skill_generator.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/skill_generator.py) 前 200 行，跟 `agents/s02-skill-md/skill.go` 对照看。再交叉看 [`anygen/agent-harness/cli_anything/anygen/skills/SKILL.md`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/anygen/agent-harness/cli_anything/anygen/skills/SKILL.md)，体会 generator 在实际产物里写出来的样子。重点段：

- **`_canonical_skill_name`**——解释为啥 `name` 是唯一硬要求的字段；缺了它 agent 的 skill discovery 层就没 key 可用。
- **`extract_intro_from_readme`**——上游用 `>-` 折叠标量包长描述；这正是我们 `raw` 字节 round-trip 要保的情况。
- **`triggers` 字段**——很多真实 skill 都带（见 `anygen` 的 SKILL.md），但可选。我们的 `yaml:"triggers,omitempty"` tag 跟这约定一致。

离线副本在 [`upstream-readings/s02-skill-md.py`](../../upstream-readings/s02-skill-md.py)。
