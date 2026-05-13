---
title: "s09 · anygen — 远程 API harness 案例"
chapter: 09
slug: s09-anygen-remote
est_read_min: 8
---

# s09 · anygen — 远程 API harness 案例

> 这一章教什么：CLI-Anything 的 harness 不一定要包本地 GUI。`anygen` 包的是一个 HTTP API —— 真正的工作发生在服务器上，CLI 只负责提交 prompt、轮询状态、取回结果。最有意思的部分是轮询循环 —— 因为本地 GUI 类型的 harness 根本不需要这一段。

## Problem

s01..s05 都默认被包装的东西就在本机：fork 子进程、读 stdout、退出。对 **AnyGen** 这种云服务，这套模型直接破产。没有可执行文件可以 fork，没有 PID 可以发信号，没有 stdout 可以排空。CLI 必须：

1. POST 一个 prompt 到 `https://www.anygen.io/v1/openapi/tasks`。
2. 立刻拿回一个 `task_id` —— 但活儿**还没开始干**。
3. 按一定间隔轮询 `GET /v1/openapi/tasks/:id`，直到 status 变成 `completed` 或 `failed`。
4. 从服务器返回的签名 URL 下载产物。

上游的 `anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py`（Python，约 430 行）用 `requests` 把这套都做了。我们要做的是用 Go 端口出这个**形状** —— 不必把整个 OpenAPI 表面都搬过来，只搬"和本地 harness 不一样的那段生命周期"。

## Solution

`client.go` 里三个原语、`poller.go` 里一个循环、`main.go` 里四个子命令：

```
APIClient.SubmitJob   (ctx, prompt)  -> jobID
APIClient.PollStatus  (ctx, jobID)   -> Status      // queued | running | succeeded | failed
APIClient.FetchResult (ctx, jobID)   -> JobResult
WaitForResult         (ctx, client, jobID, interval) -> ResultStatus
```

四个值得点名的设计决策：

1. **`Status` 是 string，不是 `int` 枚举。** 上游服务才是单一事实源；如果它下个季度加了 `cancelled`，harness 不该需要重新编译。`Status.IsTerminal()` 是 poller 唯一依赖的谓词。
2. **`PollStatus` 不把 `failed` 当错误。** 返回 `(StatusFailed, nil)` 让轮询层保持纯净：一次网络往返，没有 policy。policy（"`failed` 当致命错误处理"）放在上一层，`WaitForResult` 里。
3. **`WaitForResult` 先轮询再睡。** 一个秒完的任务（缓存命中、no-op）不该先付一个完整 interval 的延迟。循环是 `轮询 → 终态分支 → 睡 → 重复`，不是 `睡 → 轮询`。
4. **Context 取消同时打断睡眠和下一次 HTTP 调用。** `select { case <-ctx.Done(): ... case <-time.After(interval): }` 处理睡眠；`http.NewRequestWithContext` 处理在途请求。agent 撞到 timeout 时必须能真的退出。

## How It Works

```text
anygen submit "AI trends presentation"
  ├─ POST /jobs       { "prompt": "AI trends presentation" }
  └─ ◀  { "job_id": "job-xyz" }

anygen status job-xyz
  ├─ GET  /jobs/job-xyz
  └─ ◀  { "job_id": "job-xyz", "status": "running" }

anygen wait job-xyz --interval 3s --timeout 20m
  │
  │   ┌─────────────────────────────────────────┐
  │   │ for {                                   │
  │   │   status := PollStatus(ctx, jobID)      │
  │   │   if 终态:                              │
  │   │     succeeded: 返回 FetchResult()       │
  │   │     failed:    返回错误                 │
  │   │   select <-ctx.Done() | <-time.After()  │
  │   │ }                                       │
  │   └─────────────────────────────────────────┘
  └─ ◀  { "status": "succeeded", "output": "...", "content_type": "..." }
```

`poller.go` 的核心：

```go
func WaitForResult(ctx context.Context, c *APIClient, jobID string, interval time.Duration) (ResultStatus, error) {
    if interval <= 0 {
        interval = time.Second
    }
    for {
        status, err := c.PollStatus(ctx, jobID)
        if err != nil {
            if ctx.Err() != nil {
                return ResultStatus{}, ctx.Err()
            }
            return ResultStatus{}, fmt.Errorf("poll: %w", err)
        }
        switch status {
        case StatusSucceeded:
            res, err := c.FetchResult(ctx, jobID)
            if err != nil {
                return ResultStatus{Status: status}, fmt.Errorf("fetch: %w", err)
            }
            return ResultStatus{Status: status, Result: res}, nil
        case StatusFailed:
            return ResultStatus{Status: status}, fmt.Errorf("job %s failed", jobID)
        }
        select {
        case <-ctx.Done():
            return ResultStatus{}, ctx.Err()
        case <-time.After(interval):
        }
    }
}
```

三个不太显眼的点：

1. **`if ctx.Err() != nil` 在 fmt.Errorf 包装之前解开。** 被取消的 HTTP 请求会冒出一个带着噪音 URL 字符串的 `*url.Error`。我们把它特判一下，让调用方直接看到 `context.DeadlineExceeded`（用 `errors.Is` 可以判定），而不是一坨乱七八糟的字符串错误。
2. **`StatusFailed` 直接短路，不会去调 `/result`。** 上游约定是失败原因放在 status 响应自身里；对一个失败的任务调 `/result` 是未定义行为。在 status 这一步就退出更便宜也更清晰。
3. **没有指数退避。** 固定间隔和上游的 `POLL_INTERVAL = 3` 一致，也是 AnyGen 团队文档建议的做法。要退避就在调用方包一层 —— `WaitForResult` 小到能轻松外包。

## What Changed (vs. s01..s05)

两个结构性差异：

- **完全没有子进程。** s01 的 `exec.Command` 被 `http.NewRequestWithContext` 取代。harness 变成 `net/http` 上的一层薄胶水；没有 PID 要 wait，没有 stdin/stdout 管道，没有信号处理。
- **`Result` 改名 `JobResult` 避免和 CLI 信封撞车。** s01 的 `Result`（`{ok, data, error}` 信封）维持不变；AnyGen 的产物有自己的类型。一个包里两个 `Result` 会逼其中一个用 alias —— 显式命名更便宜。

前几章的一个模式被原样复用：`cli.go` 里的 `CLI` / `Flag` / `Dispatch` 三件套和 s05 的形状一样。对调用它的 agent 来说，远程 harness **仍然是一个 CLI**；只是 `Run` 函数的*函数体*变了。

## Try It

```bash
cd agents/s09-anygen-remote
make build
make demo            # 进程内 httptest.Server —— 不联网、不需要 API key
```

`make demo` 会跑一遍完整生命周期：

```text
demo: in-process AnyGen server at http://127.0.0.1:xxxxx
submit -> job_id = demo-job-001
poll  -> status = running
poll  -> status = running
poll  -> status = succeeded
fetch -> output = https://fake.anygen.io/files/demo-job-001.pptx
fetch -> content_type = application/vnd.openxmlformats-officedocument.presentationml.presentation
demo: OK
```

打真实服务：

```bash
export ANYGEN_API_KEY=sk-xxx
./s09-anygen-remote submit "季度业务总结演示稿"
./s09-anygen-remote wait <jobID> --interval 3s --timeout 20m
```

`make test` 跑五个 `httptest.Server` 测试：

- `SubmitJob_ReturnsJobID` —— POST body + auth header + 解析出来的 jobID。
- `PollStatus_ReturnsStatus` —— status 往返。
- `WaitForResult_Succeeds` —— running → running → succeeded → fetch。
- `WaitForResult_Fails` —— 终态失败短路，不会调 `/result`。
- `WaitForResult_RespectsTimeout` —— context deadline 在 1 秒内打断循环。

## Upstream Source Reading

把 [`anygen/agent-harness/ANYGEN.md`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/anygen/agent-harness/ANYGEN.md)（架构简介）和 [`anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py)（Python 实现）放一起读。重点：

- **`ANYGEN.md` "CLI Strategy: HTTP API Client"** —— 三行就把"AnyGen 为什么需要轮询"讲清楚了。我们的 Go 端口原样保留了这套策略。
- **`anygen_backend.py::poll_task`**（Python 第 291-328 行）—— 上游的轮询循环。注意它带一个 `on_progress` 回调，我们的版本省了：agent 不需要进度条，需要 UI 的话可以自己在 `WaitForResult` 外面包一层。
- **`anygen_backend.py::create_task`**（Python 第 195-268 行）—— 创建任务的 body。我们把几乎所有参数（operation、language、slide_count 等）都剥掉了，因为这一章想讲的是*生命周期*，不是 schema。真要做 anygen 的 Go 端口的话，这些字段加回来都很简单。
- **`anygen_backend.py::get_api_key`**（Python 第 63-70 行）—— 三段式认证（CLI > env > 配置文件）。我们保留前两段，砍掉了配置文件那段；真实 harness 同样可以把 `~/.config/anygen/config.json` 持久化下来。

第一页 200 行的离线副本放在 [`upstream-readings/s09-anygen-remote.md`](../../upstream-readings/s09-anygen-remote.md)。
