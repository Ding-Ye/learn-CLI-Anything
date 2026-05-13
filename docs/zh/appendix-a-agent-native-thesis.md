---
title: "附录 A · 为何 CLI 适合 agent"
slug: appendix-a-agent-native-thesis
---

# 附录 A · 为何 CLI 适合 agent

CLI-Anything 的核心论点：agent 应当通过 CLI 使用软件，而不是 GUI、不是 SDK。本附录展开"为什么"。

## 今天的软件服务于人；明天的用户是 agent

上游 README 的标语。GUI 编码的是给人的可供性——按钮放在眼会看的位置、对话框设计成可关闭、撤销缓冲适应人类反应时间。SDK 编码的是给编译器的可供性——类型检查、版本锁定、语言绑定。LLM 哪样都不合：模型没有眼动、没有编译步骤、对"版本锁"的概念也只到 prompt 这一层。

CLI 正好夹在中间。文本进，文本出，有可发现的子命令树和按需的 JSON 信封。模型原生读文本。CLI 在能 spawn 子进程的地方都能跑。接口契约就是 help string + SKILL.md。

## 为什么不直接调 SDK

三条：

1. **语言锁定。** Rust agent 调不到 OpenCV 的 Python SDK，除非移植。一个 `cv2 ...` 的 CLI 从任何有 shell 的地方都调得到。
2. **版本是 CLI 关心的事。** `imagemagick --version` 一行；`pip show pillow | grep Version` 三行。agent 一直在查 CLI 以确认对话方对不对。
3. **流式输出和 stdout 纪律。** SDK 返回对象，CLI 返回字节。agent 要从 Blender 流出 4GB 渲染视频，直接 `tail -f` stdout。要走 SDK，得学每个 SDK 各自的流式 idiom。

## 为什么不直接用 API

软件有 API 时 API 当然好用。可是长尾——Blender、FreeCAD、GIMP、Kdenlive、Audacity——根本没有。把它们的 Python 脚本接口或 D-Bus 接口包成统一 CLI，就是 CLI-Anything 的 `cli-anything-plugin/` 框架要干的活。一旦包好，agent 不需要知道底下是 CLI、SDK 还是桌面 GUI——只跑子进程。

## SKILL.md 契约

契约分三层：

- **Front-matter**（YAML）：`name`、`description`、`triggers[]`。agent runner 读这层决定**要不要**为当前用户 prompt 调这个 harness。
- **Body**（Markdown）：解释 harness 做什么、什么时候用、典型调用样例。agent 决定要用后读这层。
- **底下的 CLI**：body 教 agent 跑的那个真可执行文件。

三层，两个解析器（YAML + Markdown），一个可执行。这就是整个契约。

## CLI 让 agent 做到、其他接口做不到的事

- **跟 shell 组合。** `cli-hub install $(figure-out-name) && figure-out-name run scene.blend` 一行就完了。同样的 SDK 等价物试试。
- **缓存结果。** 内容寻址缓存（s04）在输入是"argv + 文件内容"时是 trivial 的。SDK 想做这件事需要 opt-in 仪表化。
- **沙箱容易。** 在 `firejail` / `bwrap` / Docker 里 spawn harness 就行。SDK 调用没有 syscall 边界。
- **diff 输出。** 同一个 harness 不同 args 跑两次产出两份 stdout 转储，diff 一下就是 build 比较器。SDK 对象不 diff-friendly。

## 规范刻意省掉的

- **GUI 集成。** 范围外：CLI-Anything 的任务是给现有 GUI 工具加个 CLI 让它们 agent-native，而不是发明新 GUI。
- **长任务编排。** harness 是一次性的。要 server 自己写，再写个 harness 跟 server 说话。
- **Auth 和身份。** 委托给 spawn 子进程的那一层。

## 延伸阅读

HARNESS 契约：[HARNESS.md](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/HARNESS.md)。
