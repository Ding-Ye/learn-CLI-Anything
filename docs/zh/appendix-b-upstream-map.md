---
title: "附录 B · 上游源码导读地图"
slug: appendix-b-upstream-map
---

# 附录 B · 上游源码导读地图

257K LOC 上游的导读指南，让学习者不迷路。从哪开始、承重代码在哪、第一遍可以跳过什么。

## 阅读顺序

1. `README.md`——agent-native 论点、5 分钟 quickstart、demo 长廊。
2. `cli-anything-plugin/HARNESS.md`——每个 harness 必须满足的契约。短、密、是规范本身。
3. `cli-anything-plugin/QUICKSTART.md`——自己写一个新 harness。
4. `cli-anything-plugin/skill_generator.py`——SKILL.md 生成器。看契约怎么变成 Markdown。
5. `cli-hub/cli_hub/cli.py` → `cli-hub/cli_hub/registry.py` → `cli-hub/cli_hub/installer.py`——hub 的 CLI、注册层、安装分派。按这个顺序。
6. `anygen/agent-harness/ANYGEN.md`——递归案例（anygen 包装"用来生成 harness 的 API"）。
7. 按兴趣挑一个被包装的 CLI（`blender/`、`audacity/`、`gimp/`），读它的 `SKILL.md` + Python 入口。pattern 一样，价值在领域细节。

## 各章对应上游

| 章节 | 上游文件 | 看点 |
|------|-----------|------|
| s01 min-harness | `cli-anything-plugin/HARNESS.md`、`cli-anything-plugin/templates/cli.py.j2` | 契约段（子命令树、--json 信封、退出码） |
| s02 skill-md | `cli-anything-plugin/skill_generator.py`（前 200 行） | YAML frontmatter 解析 + Markdown body 拼装 |
| s03 skill-gen | `cli-anything-plugin/skill_generator.py`（Click 内省部分） | 装饰器怎么在 runtime 被扒出来 |
| s04 preview-bundle | `cli-anything-plugin/preview_bundle.py` | fingerprint 函数 + 磁盘缓存布局 |
| s05 repl-skin | `cli-anything-plugin/repl_skin.py` | REPL 循环 + 元命令 + 祖先 SKILL.md 搜寻 |
| s06 hub-registry | `cli-hub/cli_hub/registry.py` | HTTP 拉取 + TTL 缓存 + manifest schema |
| s07 installer | `cli-hub/cli_hub/installer.py` | 后端分派器（pip/npm/uv/bundled） |
| s08 verify-plugin | `cli-anything-plugin/verify-plugin.sh`、`cli-anything-plugin/tests/` | 校验检查 + test harness pattern |
| s09 anygen-remote | `anygen/agent-harness/ANYGEN.md`、`anygen/agent-harness/anygen_backend.py` | submit/poll/result HTTP 客户端 |
| s10 publish-flow | `.github/workflows/publish-cli-hub.yml`、`.github/workflows/check-root-skills.yml` | 重新生成注册中心的 CI pipeline |

## 第一遍可以跳过

- 所有被包装的 CLI 子目录（`audacity/`、`blender/`、`chromadb/` …）。pattern 跟 `anygen/` 一样，看一个够了。
- `assets/`——视频和截图，有用但不承重。
- `QGIS/`——在迁移过程中，pattern 还不稳定。
- `.pi-extension/`——Pi 的部署集成，比 core 下游一层。

## 建议扩展练习

1. **给 s07 加真沙箱。** 现在 installer 直接把 tarball 解到安装目录。把解压包在 `firejail` 或 `bwrap` 里，限制文件系统访问只在 install dir。
2. **给 s07 加 lockfile。** 用 `golang.org/x/sys/unix.Flock` 包住 ledger 写入，防止两个并行 `hub install` 互相破坏 file。
3. **给 s09 加 SSE。** anygen 上游同时支持 poll 和 SSE。写个 `WaitForResultSSE` 订阅 server-sent-event 流而不是轮询。
4. **给 s10 加签名 / 验签。** 现在 pipeline 只写 `.sha256`，加 `cosign`-style 签名生成 + s07 装机时校验。
5. **把 s04 改成按文件元数据指纹。** 上游用 `(path, size, mtime_ns)` 来加速；Go 版按内容哈希。在 `--fast` 后切到上游做法，1GB 输入跑个性能对比。

## 许可证说明

上游 Apache-2.0。`upstream-readings/` 下的切片保留 Apache-2.0；Go 复刻是 MIT（看 LICENSE）。从本仓库摘代码进你自己的项目时，Go 文件是 MIT，逐字上游副本是 Apache-2.0。
