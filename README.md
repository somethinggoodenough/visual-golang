# Go Canvas · Go 并发可视化编辑器

**简体中文** | [English](README.en.md)

## 项目初衷 · Why I Started This Project

**中文**

学完 EECS 491 之后，我突然觉得 Go 里的 channel、goroutine 这些概念很有画面感：一个个 goroutine 各自忙碌，channel 就像把它们连接起来的管道，数据在其中流来流去。于是我就想，能不能做一个小小的可视化，把这些连接和交互画出来，让我能看一看、拖一拖、连一连，更直观地理解它们？

这个项目就从这个小念头开始。现在它还很小，未来我希望慢慢把它做得更完整、更好玩，让探索 Go 并发也成为一件可以动手尝试、享受其中的事。

**English**

After taking EECS 491, I started picturing Go’s channels and goroutines as a little network of pipes: goroutines doing their own work, channels connecting them, and data flowing from place to place. That image stuck with me, and I wondered if I could turn it into a small visualization—something I could look at, rearrange, and connect to get a better feel for how it all fits together.

This project grew out of that little idea. It’s still small, but over time I’d love to make it more complete and more fun, turning the process of exploring Go concurrency into something hands-on and enjoyable.

## 项目简介

按照 [设计文档](docs/design.md) 实现的 v0 本地工具：将受限 Go 程序导入结构图，编辑 channel、goroutine 和有序操作，再生成并使用本机 Go 工具链编译验证。

## 安装与启动

需要 **Go 1.24+**、**Node.js 22.12+** 和 npm。Go 必须位于 `PATH`。不需要数据库、账号或 API key。

```sh
# 在本目录执行；安装使用 frontend/package-lock.json 的固定依赖。
npm --prefix frontend ci
npm run dev
```

打开 **http://127.0.0.1:5173**。后端在 `127.0.0.1:8080`，Vite 将 `/api` 代理到它。Ctrl+C 关闭两端。首次启动会编译 Go 服务，缓存位于 `.cache/`。

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

## 完整编辑流程

1. 打开示例或导入 `examples/demo.go`，观察 main、goroutine、channel 资源及三个不同含义的边。
2. 选择发送操作，将整数字面量 `42` 改为 `100`，点击“应用图形修改”。源码由 IR 实际生成。
3. 点击“编译验证”。结果来自真实 `go build`；编译过程不执行用户程序。
4. 导出 Go 再导入，可以恢复相同的语义结构。导出 `.goviz.json` 项目可同时恢复节点 ID、布局、视口及原始导入源码副本。
5. 也可新建空项目，选择目标函数和插入位置，依次添加 channel、goroutine、发送、接收及打印；通过资源端口连接收发操作，设置接收变量及打印表达式，最后应用。

源码草稿与图形草稿互斥。错误或不支持的代码不会覆盖最近有效图形；先应用或明确放弃当前草稿，再编辑另一侧或保存。布局拖动不改变执行顺序，调整顺序请使用属性区的排序操作。撤销/重做同时覆盖语义和布局操作。

首次应用图形修改会规范化格式，原始导入源码作为项目副本保存。点击节点可定位源码，源码光标也可定位节点，包含 UTF-8 中文注释的情况。

## v0 支持范围

单文件 `package main`、一个无参无返回值 `main`；`chan int` 创建、发送、单值/双值/忽略绑定接收、关闭；可嵌套的无参匿名 goroutine；int/bool 字面量与局部变量；单参数 `fmt.Println` 和无值 `return`。子 goroutine 只能捕获外层 channel。整数字面量在 JSON 中使用十进制字符串，支持宿主架构 int 的范围。

未使用变量、悬空引用、非法遮蔽绑定、声明前使用和不正确类型均有诊断。合法但超出子集的程序返回 `unsupported`，语法或类型错误返回 `invalid`，均保留源码。

默认容量上限为 1024、IR 节点上限为 500、源码上限为 1 MiB。节点计数包括程序、函数、块、语句、表达式和符号。前两者可在后端启动时用 `-max-capacity` / `-max-nodes` 配置；编译超时默认 30 秒，可用 `-build-timeout 60s` 配置。

v0 不提供模拟、运行用户程序、线程时间线、trace、循环、分支、select 或多文件项目。编译成功只说明工具链接受程序，不证明不会阻塞或发生运行时错误。v1–v3 的边界见设计文档。

## 验证

```sh
npm test                          # Go 测试、真实编译用例、TypeScript 检查及前端构建
npm run test:e2e                  # 浏览器关键交互测试（见下）
npm run test:smoke                # npm run build 后验证生产服务与静态资源
```

浏览器测试使用 Playwright。首次需安装 Chromium：在 `frontend/` 中执行 `npx playwright install chromium`；也可设置 `PLAYWRIGHT_CHROMIUM_EXECUTABLE` 为已有 Chrome/Chromium 可执行文件路径。测试自动启动独立端口上的服务。

实际测试环境、验收结果和限制记录在 [implementation-status.md](docs/implementation-status.md)。

## API 与代码位置

`GET /api/health` 返回实际工具链版本。JSON POST 请求需要 `requestId`；`/api/import` 接收 `source`，`/api/generate` 和 `/api/build` 接收 `ir` 与 `programRevision`。`/api/project/validate` 接收 `project`，校验 IR、源码 hash、规范化结构与布局引用，重新建立 source map 后返回项目。

后端只使用 Go 标准库。编译使用隔离临时目录并清理产物，关闭 cgo、远程依赖、自动工具链下载、用户 Go flags 和工作区干扰。前端没有假编译或假运行结果。

- `backend/internal/engine/`：IR JSON、验证、Go 导入/生成、规范化和源码映射。
- `backend/internal/compiler/`：有超时及输出限制的真实编译。
- `backend/internal/api/`：API、项目一致性校验与映射重建。
- `frontend/src/`：模型命令、草稿/撤销状态、React Flow 画布、属性与源码编辑。
- `examples/`：可编辑示例、规范 IR fixture 以及 invalid/unsupported 用例。
