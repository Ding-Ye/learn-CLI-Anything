# learn-CLI-Anything

> 用 Go 渐进式重建 [HKUDS/CLI-Anything](https://github.com/HKUDS/CLI-Anything) 的核心 harness 模式——每章一个机制，每章末尾有上游源码导读。教学法借鉴 [shareAI-lab/learn-claude-code](https://github.com/shareAI-lab/learn-claude-code)。

English version: [README.en.md](./README.en.md).

## 这是什么 / What

**CLI-Anything** 是 HKUDS 的"让所有软件 agent-native"框架——把 GUI/SDK 工具包装成一致的 CLI + SKILL.md，让 LLM agent 能像调函数一样调它们。上游仓库 257K LOC，但 95% 在 60+ 个被包装的 CLI 子目录里（blender, audacity, anygen 等），按同一种 harness pattern 复用——真正的核心框架（`cli-anything-plugin/` + `cli-hub/`）只有 ~6K LOC。

本仓库的目标：**用 Go 从零渐进重建那个 6K 的核心 harness pattern**——每章一个机制（HARNESS 契约、SKILL.md 解析、Skill generator、preview bundle、REPL skin、CLI-Hub 注册中心、安装器、验证桩、anygen 远程 API 案例、发布流水线）。

每章 ≤ 1000 行 Go，每章是独立 Go module（`agents/sNN-*/`，无 cross-import）。

## Curriculum

| #     | 章节                                        | 状态 |
|-------|---------------------------------------------|------|
| s01   | 最小 harness：CLI + JSON 输出               | ✅   |
| s02   | SKILL.md 解析与渲染                         | ⏳   |
| s03   | 从 CLI 自动生成 SKILL.md                    | ⏳   |
| s04   | 预览包：指纹与缓存                          | ⏳   |
| s05   | REPL 外壳：交互式 harness                   | ⏳   |
| s06   | CLI-Hub 注册中心                            | ⏳   |
| s07   | 多后端安装器                                | ⏳   |
| s08   | 插件验证与测试桩                            | ⏳   |
| s09   | anygen：远程 API harness 案例               | ⏳   |
| s10   | 发布流：CI + 注册中心同步                   | ⏳   |
| s_full| 端到端集成穿刺                              | ⏳   |
| App A | 附录 A · 为何 CLI 适合 agent                | ⏳   |
| App B | 附录 B · 上游源码导读地图                   | ⏳   |

## Quickstart

```bash
git clone https://github.com/Ding-Ye/learn-CLI-Anything
cd learn-CLI-Anything
go work sync

cd agents/s01-min-harness
make demo        # human output
make demo-json   # JSON envelope an agent sees
```

需要 Go 1.22+。

## 致谢 / Acknowledgements

- 上游：[HKUDS/CLI-Anything](https://github.com/HKUDS/CLI-Anything)（Apache-2.0）。
- 教学法：[shareAI-lab/learn-claude-code](https://github.com/shareAI-lab/learn-claude-code)。
- 生成工具：Anthropic's `learn-repo-generator` skill。

## License

MIT — 见 [LICENSE](./LICENSE)。
