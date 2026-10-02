# Go Canvas · Go 并发可视化编辑器

**简体中文** | [English](README.md)

## 项目初衷 · Why I Started This Project

**中文**

学完 EECS 491 之后，我突然觉得 Go 里的 channel、goroutine 这些概念很有画面感：一个个 goroutine 各自忙碌，channel 就像把它们连接起来的管道，数据在其中流来流去。于是我就想，能不能做一个小小的可视化，把这些连接和交互画出来，让我能看一看、拖一拖、连一连，更直观地理解它们？

这个项目就从这个小念头开始。现在它还很小，未来我希望慢慢把它做得更完整、更好玩，让探索 Go 并发也成为一件可以动手尝试、享受其中的事。

**English**

After taking EECS 491, I started picturing Go’s channels and goroutines as a little network of pipes: goroutines doing their own work, channels connecting them, and data flowing from place to place. That image stuck with me, and I wondered if I could turn it into a small visualization—something I could look at, rearrange, and connect to get a better feel for how it all fits together.

This project grew out of that little idea. It’s still small, but over time I’d love to make it more complete and more fun, turning the process of exploring Go concurrency into something hands-on and enjoyable.

## 项目简介

本地 Go 可视化编辑器与执行探索器：在图形中编辑受支持的子集，或在源码优先的探索界面分析较复杂的并发程序，使用本机 Go 工具链编译、运行并记录真实 runtime trace。原有编辑模型遵循 [设计文档](docs/design.md)。

## 下一里程碑：Go 并发执行探索器

下一阶段将加入源码感知的调试与执行探索能力。源码与 Program IR 保持核心地位，图形布局、运行状态和执行事件使用独立模型。“编译”回答程序是否合法，“运行 + Trace”记录一次真实执行，后续“模拟”才探索其他调度。一次执行记录不代表所有可能执行。

计划按以下顺序推进，实际完成情况与接续位置见 [开发进度](docs/development-progress.md)：

1. **编辑器体验**：Monaco、Go 高亮、缩进、括号/引号补全、查找替换、诊断、源码 ↔ 图形定位、图形草稿只读，以及基于 `go/format` 的 `POST /api/format`。
2. **并发优先的结构扩展**：方法、循环、分支、switch/select case、map、赋值、字段、channel 参数/字段和调用；暂不分析的调用保留为不透明操作，不为每个 AST 表达式生成图块。
3. **真实运行**：独立 runner，显示输出、退出码和耗时，限制超时/输出，使用临时目录，区分编译错误、运行错误和死锁。
4. **Trace 与执行事件模型**：采集真实 Go runtime trace，独立版本的执行事件，关联源码/IR；导出源码不包含内部插桩。
5. **回放**：goroutine 泳道、重置/前一步/后一步/播放/暂停、事件选择、源码高亮，以及基于证据的阻塞解释。
6. **因果关系**：区分程序顺序、通信配对和同步，只根据证据推导 happens-before 路径；不根据相同值猜测配对，也不把时间戳顺序当作因果保证。
7. **语义缩放与检查器**：架构/控制流/语句层级，执行和因果视图，channel/goroutine/状态检查器，以及问题/输出/Trace/解释面板。
8. **后续阶段**：自定义调度模拟器、共享内存、mutex、atomic 和竞态/可见性教学。完整 Go 语法和 gopls 不属于近期范围。

验收目标是逐步追踪 KVStore 风格 APPEND 请求经过工作循环、状态循环、响应与回复 channel 的路径，并解释有证据支持的因果链。用户提供的路线图没有附原始 KVStore 源码，因此先使用独立可运行的示例。上述内容是路线图，不表示所有功能已经实现。本地进程运行适用于可信本地程序，不是执行不可信远程代码的安全沙箱。

### 目前可用

- Monaco 源码编辑器：本地 Worker、高亮、诊断、双向定位、查找替换，以及可撤销的 Go 格式化（`Shift+Alt+F`）。`Ctrl/Cmd+Enter` 应用源码草稿；在探索界面中执行源码分析。
- **执行探索**：结构 / 执行 / 因果视图，问题 / 输出 / Trace / 解释面板。架构、控制流、语句层级可识别方法、循环、select/switch、map、字段、调用和 channel 声明。扩展结构目前只读；可编辑图形仍遵循下方 v0 子集。
- 对可信、标准库、单文件 `package main` 程序进行编译、运行及运行 + Trace，显示真实 stdout/stderr、退出码、耗时、源码 hash，支持取消并区分编译失败、运行错误、死锁和超时。
- 真实 goroutine 状态变化、语句注解、逐步回放、源码/结构定位，以及原始 trace 下载。回放不能控制调度；目前因果路径仅使用 goroutine 内程序顺序和 goroutine 创建同步。

**尚未完成**：精确 channel 运行实例与收发配对、buffer/值和共享状态检查、channel 同步路径，以及扩展语法的图形编辑。不根据相同值或时间戳猜测通信与 happens-before。完整里程碑尚未达成。

## 安装与启动

需要 **Go 1.24+**、**Node.js 22.12+** 和 npm。Go 必须位于 `PATH`。不需要数据库、账号或 API key。

```sh
# 在本目录执行；安装使用 frontend/package-lock.json 的固定依赖。
npm --prefix frontend ci
npm run dev
```

打开 **http://127.0.0.1:5000**（或使用 Replit 预览）。后端在 `127.0.0.1:8080`，Vite 将 `/api` 代理到它。Ctrl+C 关闭两端。每次 `npm run dev` 启动都会编译 Go 服务，缓存位于 `.cache/`；运行期间修改 Go 后端代码，需要重启开发服务。

Windows PowerShell 如果拦截 `npm.ps1`，使用 `npm.cmd` 即可，无需修改系统执行策略。使用已经下载到本项目的 Go 时：

```powershell
$env:Path = "$PWD\.cache\go1.24.13\go\bin;$env:Path"
npm.cmd run dev
```

生产构建与本地运行：

```sh
npm run build
npm start
```

打开 **http://127.0.0.1:8080**，由 Go 服务直接提供构建后的前端。服务限制为 loopback。

也可以分开启动：

```sh
# 终端 1
cd backend
GOTOOLCHAIN=local go run ./cmd/server

# 终端 2，从项目根目录出发
cd frontend
npm run dev
```

### 执行探索提示「API 不存在」

如果图形编辑显示「本地服务已连接」，执行探索却提示「API 不存在」，可能是新前端连接到了仍在运行的旧后端。Vite 会热更新前端，但 `scripts/dev.mjs` 只在启动时编译后端，不监听 Go 文件变化。旧进程可能没有 `/api/analyze`、`/api/run`、`/api/build-source` 和 `/api/format`；`/api/health` 返回成功只表示服务可连接，不证明这些新接口已加载。

开发模式的恢复步骤：

1. 先导出 Go，保存需要保留的源码草稿。
2. 在原来启动服务的终端按 `Ctrl+C`，确认该项目的旧服务已退出。如果重新启动时提示端口占用，先核对占用 `8080` 或 `5000` 的进程是否属于本项目，再停止对应旧进程；不要批量结束所有 Node 或 Go 进程。
3. 在项目根目录 PowerShell 中重新启动（以下路径适用于已下载到本项目的 Go）：

   ```powershell
   $env:Path = "$PWD\.cache\go1.24.13\go\bin;$env:Path"
   npm.cmd run dev
   ```

4. 等待后端和 Vite 启动完成，再刷新浏览器，进入「执行探索」并重新「分析源码」。

可在另一个 PowerShell 终端验证分析接口；此请求只分析源码，不运行程序：

```powershell
$probeBody = @{ requestId = 'probe'; source = "package main`nfunc main() {}" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8080/api/analyze' -ContentType 'application/json' -Body $probeBody
```

预期返回 `status: ok` 和 `requestId: probe`。开发模式下将地址中的端口改为 `5000`，还可以验证 Vite 代理。必须使用 `POST`；在浏览器地址栏打开该接口发送的是 `GET`，不能据此判断分析接口是否可用。

生产模式需先停止旧服务，再依次执行 `npm.cmd run build` 和 `npm.cmd start`，然后刷新 `http://127.0.0.1:8080`。仅刷新浏览器或重新构建文件，都不会替换已经运行的 Go 后端进程。

## 完整编辑流程

1. 打开示例或导入 `examples/demo.go`，观察 main、goroutine、channel 资源及三个不同含义的边。
2. 选择发送操作，将整数字面量 `42` 改为 `100`，点击“应用图形修改”。源码由 IR 实际生成。
3. 点击“编译验证”。结果来自真实 `go build`；编译过程不执行用户程序。
4. 导出 Go 再导入，可以恢复相同的语义结构。导出 `.goviz.json` 项目可同时恢复节点 ID、布局、视口及原始导入源码副本。
5. 也可新建空项目，选择目标函数和插入位置，依次添加 channel、goroutine、发送、接收及打印；通过资源端口连接收发操作，设置接收变量及打印表达式，最后应用。

源码草稿与图形草稿互斥。错误或不支持的代码不会覆盖最近有效图形；先应用或明确放弃当前草稿，再编辑另一侧或保存。布局拖动不改变执行顺序，调整顺序请使用属性区的排序操作。撤销/重做同时覆盖语义和布局操作。

首次应用图形修改会规范化格式，原始导入源码作为项目副本保存。点击节点可定位源码，源码光标也可定位节点，包含 UTF-8 中文注释的情况。

## 试用执行探索

1. 点击 **执行探索 → KVStore 示例 → 分析源码**，切换架构、控制流和语句层级。
2. 先点 **编译验证**，再点 **运行**。示例输出 `APPEND: Hello`、`retry: Hello`、`APPEND: Hello Go`、`GET: Hello Go` 和 `stopped`，并包含模拟磁盘写入信息。
3. 点击 **运行 + Trace**，用前一步、后一步、播放或滑块回放。选择 Trace 事件会定位源码及有精确映射的结构节点；解释面板展示运行时状态变化证据。
4. 在 **因果** 视图选择起点事件和终点步骤，查看已支持的证据路径。没有找到路径只代表采集的证据不足，不证明不存在因果关系。
5. 导出 Go 保存源码；下载原始 trace 后可用 `go tool trace execution.trace` 查看。扩展语法及执行记录暂不保存到 v0 `.goviz.json` 项目文件。

修改源码会使旧分析、运行结果失效。返回图形编辑会保留源码草稿，但超出 v0 的程序无法应用到可编辑图形。替换源码前请先导出 Go。

默认限制：编译 30 秒、运行 5 秒（`-run-timeout`）、合计输出 64 KiB、原始 trace 4 MiB、规范化事件 20,000 条。Trace 解码依赖 Go 工具链格式并有资源限制，不可用或不完整时会明确提示。插桩会影响调度；用于刷新 trace 的定时器可能把原本的死锁变成超时，因此只有真实 Go fatal-deadlock 报告才标为 `deadlock`。运行进程具有当前用户权限，以上限制**不是操作系统安全沙箱**。

## v0 支持范围

单文件 `package main`、一个无参无返回值 `main`；`chan int` 创建、发送、单值/双值/忽略绑定接收、关闭；可嵌套的无参匿名 goroutine；int/bool 字面量与局部变量；单参数 `fmt.Println` 和无值 `return`。子 goroutine 只能捕获外层 channel。整数字面量在 JSON 中使用十进制字符串，支持宿主架构 int 的范围。

未使用变量、悬空引用、非法遮蔽绑定、声明前使用和不正确类型均有诊断。合法但超出子集的程序返回 `unsupported`，语法或类型错误返回 `invalid`，均保留源码。

默认容量上限为 1024、IR 节点上限为 500、源码上限为 1 MiB。节点计数包括程序、函数、块、语句、表达式和符号。前两者可在后端启动时用 `-max-capacity` / `-max-nodes` 配置；编译超时默认 30 秒，可用 `-build-timeout 60s` 配置。

v0 **可编辑图形**不支持循环、分支或 select；这些语法可在独立的源码探索界面分析和运行。多文件项目、调度模拟尚未支持。编译成功只说明工具链接受程序，不证明不会阻塞或发生运行时错误。

## 验证

```sh
npm test                          # Go 测试、真实编译用例、TypeScript 检查及前端构建
npm run test:e2e                  # 浏览器关键交互测试（见下）
npm run test:smoke                # npm run build 后验证生产服务与静态资源
```

浏览器测试使用 Playwright。首次需安装 Chromium：在 `frontend/` 中执行 `npx playwright install chromium`；也可设置 `PLAYWRIGHT_CHROMIUM_EXECUTABLE` 为已有 Chrome/Chromium 可执行文件路径。测试自动启动独立端口上的服务。

当前验证结果与剩余工作见 [开发进度](docs/development-progress.md)；[implementation-status.md](docs/implementation-status.md) 保留之前 v0 的历史验收记录。

## API 与代码位置

`GET /api/health` 返回实际工具链版本。JSON POST 请求需要 `requestId`；`/api/import` 接收 `source`，`/api/generate` 和 `/api/build` 接收 `ir` 与 `programRevision`。`/api/project/validate` 接收 `project`，校验 IR、源码 hash、规范化结构与布局引用，重新建立 source map 后返回项目。

新增 POST 接口：`/api/format` 格式化源码草稿；`/api/analyze` 返回独立版本的只读结构模型；`/api/build-source` 只编译源码；`/api/run` 运行源码（`trace: true` 启用 trace）。后两个接口也可接收 v0 `ir` 代替 `source`，两者不能同时提供。响应回显 `requestId`、`programRevision`，并用源码 hash 绑定本次源码。请求不接受任意命令或可执行文件路径。

后端只使用 Go 标准库。编译使用隔离临时目录并清理产物，关闭 cgo、远程依赖、自动工具链下载、用户 Go flags 和工作区干扰。前端没有假编译或假运行结果。

- `backend/internal/engine/`：IR JSON、验证、Go 导入/生成、规范化和源码映射。
- `backend/internal/compiler/`：有超时及输出限制的真实编译。
- `backend/internal/analysis/`：独立于可编辑 v0 IR 的扩展源码结构投影。
- `backend/internal/runner/` 与 `backend/internal/trace/`：本地运行、trace 插桩与规范化执行事件。
- `backend/internal/api/`：API、项目一致性校验与映射重建。
- `frontend/src/`：模型命令、草稿/撤销状态、React Flow 画布、属性与源码编辑。
- `frontend/src/explorer/`：源码优先的执行工作流、语义视图、回放和证据路径。
- `examples/`：可编辑示例、规范 IR fixture 以及 invalid/unsupported 用例。
