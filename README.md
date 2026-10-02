# Go Canvas · Go Concurrency Visual Editor

[简体中文](README.cn.md) | **English**

## Why I Started This Project

**English**

After taking EECS 491, I started picturing Go’s channels and goroutines as a little network of pipes: goroutines doing their own work, channels connecting them, and data flowing from place to place. That image stuck with me, and I wondered if I could turn it into a small visualization—something I could look at, rearrange, and connect to get a better feel for how it all fits together.

This project grew out of that little idea. It’s still small, but over time I’d love to make it more complete and more fun, turning the process of exploring Go concurrency into something hands-on and enjoyable.

## Overview

A local Go visual editor and execution explorer. Edit the supported visual subset as a graph, or use the source-first explorer for larger concurrent programs. Compile, run, and record real Go runtime traces with your local toolchain. The original editing model follows the [design document (Chinese)](docs/design.md).

## Next milestone: Go concurrency execution explorer

The next direction is a source-aware debugger and execution explorer. Source and Program IR remain first-class; graph layout, runtime state, and execution events are separate models. Compile validates a program, Run + Trace records one real execution, and a future simulator will explore alternative schedules. A recorded execution is not proof of every possible execution.

Planned implementation order (progress and resume notes: [development progress](docs/development-progress.md)):

1. **Editor quality:** Monaco, Go highlighting, indentation, bracket/quote completion, search/replace, diagnostics, source ↔ graph navigation, read-only graph drafts, and `POST /api/format` using `go/format`.
2. **Concurrency-first structure:** methods, loops, branches, switch/select cases, maps, assignments, fields, channel arguments/fields, and calls. Keep opaque calls when their internals are not modeled; avoid visualizing every AST expression.
3. **Real Run:** a separate runner with output, exit status, timeouts, output limits, temporary files, and distinct compile/runtime/deadlock results.
4. **Trace capture and event model:** real Go runtime tracing, separately versioned execution events, and source/IR annotations without modifying exported source.
5. **Replay:** goroutine lanes, reset/previous/next/play/pause, event selection, source highlighting, and evidence-based blocking explanations.
6. **Causality:** distinguish program order, matched communication, and synchronization; derive happens-before paths only from evidence. Do not infer matching from values or display timestamps as causal guarantees.
7. **Semantic zoom and inspectors:** architecture/control-flow/statement views, execution and causality views, channel/goroutine/state inspectors, and Problems/Output/Trace/Explanation panels.
8. **Later:** a custom scheduler simulator, shared memory, mutexes, atomics, and race/visibility teaching. Full Go coverage and gopls are not immediate goals.

Acceptance target: follow a KVStore-style APPEND request through worker, state, response, and reply channels, select runtime steps, and explain a supported causal path. The supplied roadmap did not include the original KVStore source; a self-contained example will be used first. These items are a roadmap, not claims that every feature is implemented. Local process execution is for trusted local programs, not a sandbox for untrusted remote code.

### Available now

- Monaco source editor with local workers, diagnostics, source navigation, search/replace, and undoable Go formatting (`Shift+Alt+F`). `Ctrl/Cmd+Enter` applies a source draft or analyzes source in the explorer.
- **Execution explorer** with Structure / Execution / Causality views and Problems / Output / Trace / Explanation panels. Architecture, control-flow, and statement views recognize methods, loops, select/switch, maps, fields, calls, and channel declarations. This larger structure model is read-only; editable graphs retain the v0 subset below.
- Compile, Run, and Run + Trace for trusted, single-file `package main` programs using the standard library. Results include real stdout/stderr, exit status, duration, source hash, cancellation, and distinct build/runtime/deadlock/timeout outcomes.
- Actual runtime goroutine transitions, statement annotations, trace replay controls, linked source/structure selection, and native trace download. Replay does not control the scheduler. Causal paths currently use per-goroutine program order and goroutine-creation synchronization only.

**Still pending:** exact channel instances and send/receive pairing, channel buffer/value inspection, shared-state inspection, channel synchronization paths, and graph editing of the expanded syntax. No channel pairing or happens-before edge is inferred from matching values or timestamps. The full milestone is not yet complete.

## Installation and Startup

Requires **Go 1.24+**, **Node.js 22.12+**, and npm. Go must be on your `PATH`. No database, account, or API key is required.

```sh
# Run from this directory; dependencies are pinned by frontend/package-lock.json.
npm --prefix frontend ci
npm run dev
```

Open **http://127.0.0.1:5000** (or the Replit preview). The backend listens on `127.0.0.1:8080`, and Vite proxies `/api` requests to it. Press Ctrl+C to stop both processes. Every `npm run dev` startup compiles the Go service, with its cache stored in `.cache/`. Restart the development services after changing Go backend code.

Windows PowerShell: use `npm.cmd` if execution policy blocks `npm.ps1`; changing the system execution policy is unnecessary. If using the Go installation downloaded into this workspace:

```powershell
$env:Path = "$PWD\.cache\go1.24.13\go\bin;$env:Path"
npm.cmd run dev
```

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

### Execution explorer reports "API not found"

If the graph editor reports a connected local service but the execution explorer reports "API not found", the new frontend may be connected to an old backend process. Vite hot-reloads the frontend, but `scripts/dev.mjs` compiles the backend only at startup and does not watch Go files. The old process may lack `/api/analyze`, `/api/run`, `/api/build-source`, and `/api/format`. A successful `/api/health` response confirms connectivity, not availability of these new endpoints.

To recover in development mode:

1. Export Go to preserve any source drafts you need.
2. Press `Ctrl+C` in the terminal that started the services and confirm this project's old services have exited. If restarting reports a port conflict, identify the process listening on `8080` or `5000` and verify that it belongs to this project before stopping it. Do not terminate all Node or Go processes.
3. Restart from the project root in PowerShell (these commands use the Go installation already downloaded into this workspace):

   ```powershell
   $env:Path = "$PWD\.cache\go1.24.13\go\bin;$env:Path"
   npm.cmd run dev
   ```

4. Wait for the backend and Vite to start, refresh the browser, open the execution explorer, and analyze the source again.

Use another PowerShell terminal to check the analysis endpoint without executing a program:

```powershell
$probeBody = @{ requestId = 'probe'; source = "package main`nfunc main() {}" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8080/api/analyze' -ContentType 'application/json' -Body $probeBody
```

Expect `status: ok` and `requestId: probe`. In development mode, change the port to `5000` to check the Vite proxy as well. Use `POST`: opening this URL in the browser address bar sends `GET` and does not test whether the analysis endpoint is available.

In production mode, stop the old service first, then run `npm.cmd run build` followed by `npm.cmd start`, and refresh `http://127.0.0.1:8080`. Refreshing the browser or rebuilding files alone does not replace a running Go backend process.

## Complete Editing Workflow

1. Open an example or import `examples/demo.go` to inspect main, goroutines, channel resources, and the three kinds of edges with distinct meanings.
2. Select a send operation, change the integer literal `42` to `100`, and click “应用图形修改” (Apply Graph Changes). The source code is generated from the IR.
3. Click “编译验证” (Validate Compilation). The result comes from a real `go build`; compilation does not execute the user program.
4. Export Go code and import it again to recover the same semantic structure. Export a `.goviz.json` project to also preserve node IDs, layout, viewport, and a copy of the originally imported source code.
5. Alternatively, create an empty project, select the target function and insertion point, and add channels, goroutines, sends, receives, and print operations. Connect send and receive operations through resource ports, set receive variables and print expressions, then apply the changes.

Source drafts and graph drafts are mutually exclusive. Invalid or unsupported code does not overwrite the last valid graph. Apply or explicitly discard the current draft before editing the other representation or saving. Dragging nodes changes the layout, not execution order; use the ordering controls in the properties panel to change execution order. Undo and redo cover both semantic and layout changes.

Applying graph changes for the first time normalizes formatting. The originally imported source code is kept as a copy in the project. Clicking a node locates its source code, and the source cursor can locate the corresponding node, including when the source contains UTF-8 Chinese comments.

## Try the Execution Explorer

1. Click **Execution explorer**, then **KVStore example** and **Analyze source**. Explore the architecture, control-flow, and statement levels.
2. Click **Validate build**, then **Run**. The example prints `APPEND: Hello`, `retry: Hello`, `APPEND: Hello Go`, `GET: Hello Go`, and `stopped` (with simulated disk-write messages).
3. Click **Run + Trace**. Use next/previous/play or the replay slider; select a Trace event to highlight the source and its mapped structure node. The Explanation panel describes recorded state transitions.
4. In **Causality**, choose a starting event and a destination step to inspect a supported evidence path. An absent path means insufficient captured evidence, not proof of no causal relation.
5. Export Go to save the edited source; download the native trace for `go tool trace execution.trace`. Expanded programs and execution recordings are not stored in v0 `.goviz.json` project files.

Source edits invalidate old analysis and execution results. Returning to the graph editor preserves the source draft, but programs outside v0 cannot be applied to its editable graph. Export them as Go before replacing the source.

Execution defaults: 30-second build timeout, 5-second runtime timeout (`-run-timeout`), 64 KiB combined output, 4 MiB native trace, and 20,000 normalized events. Trace decoding is Go-toolchain-specific and bounded; unavailable or incomplete traces are reported explicitly. Instrumentation affects scheduling. Its flush timer can turn a would-be deadlock into a timeout, so only an actual Go fatal-deadlock report is labeled `deadlock`. Run executes local code with your user permissions; these limits are **not** an OS security sandbox.

## v0 Support

Supports a single-file `package main` with one `main` function that takes no arguments and returns no values; creating, sending to, receiving from, and closing `chan int` channels, with single-value, two-value, and ignored-binding receives; nested anonymous goroutines with no arguments; int/bool literals and local variables; single-argument `fmt.Println`; and `return` without a value. Child goroutines may capture only channels from an outer scope. Integer literals are represented as decimal strings in JSON and support the host architecture's int range.

Diagnostics cover unused variables, dangling references, invalid shadowing bindings, use before declaration, and incorrect types. Valid programs outside the supported subset return `unsupported`; syntax or type errors return `invalid`. The source code is preserved in both cases.

The default limits are a channel capacity of 1024, 500 IR nodes, and 1 MiB of source code. Node counts include programs, functions, blocks, statements, expressions, and symbols. The first two limits can be configured at backend startup with `-max-capacity` / `-max-nodes`. Compilation has a default timeout of 30 seconds, which can be changed with, for example, `-build-timeout 60s`.

The v0 **editable graph** does not support loops, branches, or select. Those can be analyzed and run in the separate source-first explorer. Multi-file projects and simulation remain unsupported. Successful compilation only means the toolchain accepts the program; it does not prove that the program will avoid blocking or runtime errors.

## Verification

```sh
npm test                          # Go tests, real compilation cases, TypeScript checks, and frontend build
npm run test:e2e                  # Browser tests for key interactions (see below)
npm run test:smoke                # Verify the production service and static assets after npm run build
```

Browser tests use Playwright. Install Chromium before the first run by executing `npx playwright install chromium` in `frontend/`. Alternatively, set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` to the path of an existing Chrome/Chromium executable. Tests automatically start services on separate ports.

Current verification and remaining work are recorded in [development-progress.md (Chinese)](docs/development-progress.md). [implementation-status.md (Chinese)](docs/implementation-status.md) retains the earlier v0 acceptance record.

## API and Code Locations

`GET /api/health` returns the actual toolchain version. JSON POST requests require `requestId`. `/api/import` accepts `source`; `/api/generate` and `/api/build` accept `ir` and `programRevision`. `/api/project/validate` accepts `project`, validates the IR, source hash, normalized structure, and layout references, then rebuilds the source map and returns the project.

New POST endpoints: `/api/format` formats a source draft; `/api/analyze` returns the separately versioned, read-only source structure; `/api/build-source` compiles source without execution; `/api/run` executes source (`trace: true` enables tracing). Run/build-source also accept v0 `ir` instead of `source`, never both. Responses echo `requestId` and `programRevision`; source hashes identify the exact source used. Request bodies never accept commands or executable paths.

The backend uses only the Go standard library. Compilation uses isolated temporary directories and cleans up build artifacts. It disables cgo, remote dependencies, automatic toolchain downloads, user Go flags, and workspace interference. The frontend does not fabricate compilation or execution results.

- `backend/internal/engine/`: IR JSON, validation, Go import/generation, normalization, and source mapping.
- `backend/internal/compiler/`: Real compilation with timeout and output limits.
- `backend/internal/analysis/`: Expanded, source-aware structure projection, separate from editable v0 IR.
- `backend/internal/runner/` and `backend/internal/trace/`: Local execution, trace instrumentation, and normalized execution events.
- `backend/internal/api/`: API, project consistency validation, and source map rebuilding.
- `frontend/src/`: Model commands, draft/undo state, React Flow canvas, properties, and source editing.
- `frontend/src/explorer/`: Source-first execution workflow, semantic views, replay, and evidence paths.
- `examples/`: Editable examples, canonical IR fixtures, and invalid/unsupported cases.
