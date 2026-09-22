# Go Canvas · Go Concurrency Visual Editor

[简体中文](README.md) | **English**

## Why I Started This Project · 项目初衷

**English**

After taking EECS 491, I started picturing Go’s channels and goroutines as a little network of pipes: goroutines doing their own work, channels connecting them, and data flowing from place to place. That image stuck with me, and I wondered if I could turn it into a small visualization—something I could look at, rearrange, and connect to get a better feel for how it all fits together.

This project grew out of that little idea. It’s still small, but over time I’d love to make it more complete and more fun, turning the process of exploring Go concurrency into something hands-on and enjoyable.

**中文**

学完 EECS 491 之后，我突然觉得 Go 里的 channel、goroutine 这些概念很有画面感：一个个 goroutine 各自忙碌，channel 就像把它们连接起来的管道，数据在其中流来流去。于是我就想，能不能做一个小小的可视化，把这些连接和交互画出来，让我能看一看、拖一拖、连一连，更直观地理解它们？

这个项目就从这个小念头开始。现在它还很小，未来我希望慢慢把它做得更完整、更好玩，让探索 Go 并发也成为一件可以动手尝试、享受其中的事。

## Overview

A local v0 tool built according to the [design document (Chinese)](docs/design.md). Import Go programs from a supported subset into a structural graph, edit channels, goroutines, and ordered operations, then generate Go code and validate it with your local Go toolchain.

## Installation and Startup

Requires **Go 1.24+**, **Node.js 22.12+**, and npm. Go must be on your `PATH`. No database, account, or API key is required.

```sh
# Run from this directory; dependencies are pinned by frontend/package-lock.json.
npm --prefix frontend ci
npm run dev
```

Open **http://127.0.0.1:5173**. The backend listens on `127.0.0.1:8080`, and Vite proxies `/api` requests to it. Press Ctrl+C to stop both processes. The first startup compiles the Go service, with its cache stored in `.cache/`.

To build for production and run locally:

```sh
npm run build
npm start
```

Open **http://127.0.0.1:8080**. The Go service serves the built frontend directly. The service binds only to the loopback interface.

You can also start the services separately:

```sh
# Terminal 1
cd backend
GOTOOLCHAIN=local go run ./cmd/server

# Terminal 2, starting from the project root
cd frontend
npm run dev
```

## Complete Editing Workflow

1. Open an example or import `examples/demo.go` to inspect main, goroutines, channel resources, and the three kinds of edges with distinct meanings.
2. Select a send operation, change the integer literal `42` to `100`, and click “应用图形修改” (Apply Graph Changes). The source code is generated from the IR.
3. Click “编译验证” (Validate Compilation). The result comes from a real `go build`; compilation does not execute the user program.
4. Export Go code and import it again to recover the same semantic structure. Export a `.goviz.json` project to also preserve node IDs, layout, viewport, and a copy of the originally imported source code.
5. Alternatively, create an empty project, select the target function and insertion point, and add channels, goroutines, sends, receives, and print operations. Connect send and receive operations through resource ports, set receive variables and print expressions, then apply the changes.

Source drafts and graph drafts are mutually exclusive. Invalid or unsupported code does not overwrite the last valid graph. Apply or explicitly discard the current draft before editing the other representation or saving. Dragging nodes changes the layout, not execution order; use the ordering controls in the properties panel to change execution order. Undo and redo cover both semantic and layout changes.

Applying graph changes for the first time normalizes formatting. The originally imported source code is kept as a copy in the project. Clicking a node locates its source code, and the source cursor can locate the corresponding node, including when the source contains UTF-8 Chinese comments.

## v0 Support

Supports a single-file `package main` with one `main` function that takes no arguments and returns no values; creating, sending to, receiving from, and closing `chan int` channels, with single-value, two-value, and ignored-binding receives; nested anonymous goroutines with no arguments; int/bool literals and local variables; single-argument `fmt.Println`; and `return` without a value. Child goroutines may capture only channels from an outer scope. Integer literals are represented as decimal strings in JSON and support the host architecture's int range.

Diagnostics cover unused variables, dangling references, invalid shadowing bindings, use before declaration, and incorrect types. Valid programs outside the supported subset return `unsupported`; syntax or type errors return `invalid`. The source code is preserved in both cases.

The default limits are a channel capacity of 1024, 500 IR nodes, and 1 MiB of source code. Node counts include programs, functions, blocks, statements, expressions, and symbols. The first two limits can be configured at backend startup with `-max-capacity` / `-max-nodes`. Compilation has a default timeout of 30 seconds, which can be changed with, for example, `-build-timeout 60s`.

v0 does not provide simulation, execution of user programs, thread timelines, traces, loops, branches, select, or multi-file projects. Successful compilation only means the toolchain accepts the program; it does not prove that the program will avoid blocking or runtime errors. See the [design document (Chinese)](docs/design.md) for the scope of v1–v3.

## Verification

```sh
npm test                          # Go tests, real compilation cases, TypeScript checks, and frontend build
npm run test:e2e                  # Browser tests for key interactions (see below)
npm run test:smoke                # Verify the production service and static assets after npm run build
```

Browser tests use Playwright. Install Chromium before the first run by executing `npx playwright install chromium` in `frontend/`. Alternatively, set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` to the path of an existing Chrome/Chromium executable. Tests automatically start services on separate ports.

The actual test environment, acceptance results, and limitations are recorded in [implementation-status.md (Chinese)](docs/implementation-status.md).

## API and Code Locations

`GET /api/health` returns the actual toolchain version. JSON POST requests require `requestId`. `/api/import` accepts `source`; `/api/generate` and `/api/build` accept `ir` and `programRevision`. `/api/project/validate` accepts `project`, validates the IR, source hash, normalized structure, and layout references, then rebuilds the source map and returns the project.

The backend uses only the Go standard library. Compilation uses isolated temporary directories and cleans up build artifacts. It disables cgo, remote dependencies, automatic toolchain downloads, user Go flags, and workspace interference. The frontend does not fabricate compilation or execution results.

- `backend/internal/engine/`: IR JSON, validation, Go import/generation, normalization, and source mapping.
- `backend/internal/compiler/`: Real compilation with timeout and output limits.
- `backend/internal/api/`: API, project consistency validation, and source map rebuilding.
- `frontend/src/`: Model commands, draft/undo state, React Flow canvas, properties, and source editing.
- `examples/`: Editable examples, canonical IR fixtures, and invalid/unsupported cases.
