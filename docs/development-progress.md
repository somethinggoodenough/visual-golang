# 开发进度与接续记录

本轮需求：先把 `visual-golang-next-steps.md` 的方向更新到中英文 README，再按优先级实现。该文档是功能设计输入；其中后期模拟器、共享内存和 gopls 不作为立即交付项。

## 当前状态

- [x] 中英文 README 记录产品方向、路线图、现有能力、使用方法与边界。
- [x] Monaco 编辑器、格式化、诊断与双向定位，保留草稿只读和格式化撤销/重做。
- [x] 扩展语法的只读并发结构分析及独立 KVStore APPEND 示例。
- [x] 独立 runner，真实输出、退出状态、取消、超时和输出限制。
- [x] runtime trace 采集、独立执行事件模型、goroutine 泳道与逐步回放，通过真实 KVStore 集成验证。
- [x] 程序顺序/goroutine 创建同步路径与证据解释。
- [x] 本轮集成验证和接续记录；不代表完整路线图已完成。
- [ ] 精确 channel 运行实例、通信配对、buffer/值与共享状态检查。
- [ ] Channel 同步及跨请求/响应 channel 的 happens-before 路径。
- [ ] 扩展语法的可编辑 IR 和项目 schema 迁移。

## 关键约束

- 保留已实现的发送橙色、接收蓝色及双语图例。
- 保留 v0 可编辑 IR 的草稿互斥、撤销和项目恢复。
- 静态结构与执行模型独立；运行结果绑定源码 hash，不用旧运行覆盖新源码。
- 不把时间戳或相同 payload 当成通信证据，不把 trace 回放当成调度控制。
- 使用项目本地 `.cache/go1.24.13/go/bin/go.exe`；Windows 使用 `npm.cmd`。
- 可信本地运行具有当前用户权限，不是文件系统、网络或子进程安全沙箱。

## Credit 与停止约定

当前工具不提供账户 credit 百分比，无法自动检测 10%。如用户通知接近阈值，将停止工作并记录已完成、未完成、最近验证命令和准确接续位置。不得把 token/context 余量当作账户 credit。本次检查点不是因确认 credit 已到 10% 而触发。

## Left off

本轮停靠点：源码编辑 → 扩展只读结构 → 真实编译/运行 → Trace → 回放/定位这一条基础流程已落地并验证。没有待修复的已知失败测试。保留了用户先前的收发引用配色改动。

手动测试遇到的“API 不存在”已定位为旧后端进程未重启。更新开发服务后，已通过实际网页端口 5000 验证 KVStore 分析、格式化、编译、运行与 Trace；原因及恢复步骤已补入中英文 README。图形编辑无法导入 KVStore 的 v0 子集限制仍待后续扩展。

从主界面点击 **执行探索 → KVStore 示例 → 分析源码 → 运行 + Trace** 即可试用。`examples/kvstore.go` 是独立请求/响应示例，不是用户原始 KVStore 的恢复副本。

关键位置：

- `frontend/src/editor/SourceEditor.tsx`：Monaco 生命周期、编辑撤销、诊断和定位。
- `frontend/src/explorer/useExplorer.ts`：分析/编译/运行请求、取消、hash 校验与过期结果保护。
- `frontend/src/explorer/StructureView.tsx`：只读语义层级和静态 Channel 访问关系。
- `frontend/src/explorer/ExecutionView.tsx`：回放、goroutine 状态、阻塞解释和因果查询。
- `frontend/src/explorer/model.ts`：独立事件类型与因果路径；未知显式 nodeId 不按行误匹配。
- `backend/internal/analysis/`：方法、控制流、map 与声明级 channel 引用。
- `backend/internal/runner/`：实际进程、标准库限制、资源边界及 UTF-8 编译诊断。
- `backend/internal/trace/`：临时源码插桩、runtime trace 解码、原始字节位置映射。
- `backend/internal/api/format.go`、`explorer.go`：新增接口；IR 展开后也检查 1 MiB 源码上限。

### 下一步从哪里继续

1. 先设计 ChannelInstance / Communication 事件合同及可验证的插桩覆盖范围。当前 channelId 只是静态声明，不是运行时对象；同一字段在多个 struct 实例上不能合并为一个实际 channel。
2. 精确配对必须有运行时身份证据，覆盖缓冲、多发送者、多接收者、select、关闭及别名。不能根据相同值、相邻日志、唤醒者或时间戳猜测配对。
3. 拿到精确配对后实现 channel 同步边、buffer/等待者检查器及跨 writeLoop/stateLoop 的 APPEND 因果路径。目前只支持程序顺序和 goroutine 创建同步；依据为 [Go 内存模型](https://go.dev/ref/mem)。
4. 扩展可编辑 Program IR / 项目 schema 时，保留 v0 文件兼容、语义往返、hash/修订、草稿互斥与撤销。当前大程序只能作为 Go 源码导出，不能保存为 v0 图形项目。
5. 模拟器、共享内存、锁和 atomics 留在后续阶段。不得把基础回放交付标作全部里程碑完成。

### 当前 Trace 边界

- Trace 从 main 开始，不覆盖之前的包初始化。停止采集后的退出状态不在 trace 中，泳道显示最后观测状态，不保证是进程最终状态。
- 注解记录语句边界；attempt 在操作数求值前，不能单靠它判断阻塞。Go runtime state transition 才确认阻塞。
- 导出的 Go 源码不含插桩，但插桩会影响调度。helper 状态事件带 instrumentation 标记，默认回放跳过，原始事件表保留。
- 内部刷新和超时定时器可能让死锁表现为 timeout；只有实际 Go fatal deadlock 报告才标为 deadlock。
- worker panic、os.Exit、强制停止或达到上限可能留下不完整 trace；UI 提示警告，不伪造缺失事件。
- 解码优先使用 `go tool trace -d=parsed`，兼容旧 `-d=1`。未知记录保守忽略；采集/解码均有资源上限。

## 本轮验证（2026-10-02）

环境：Windows amd64、项目本地 Go 1.24.13、本机 Google Chrome。Monaco 固定为 0.57.0，Worker 随本地构建，无 CDN 依赖。

- `npm.cmd test`：全部 Go 测试包、TypeScript 检查与 Vite 构建通过。
- `npm.cmd run test:e2e`：**28/28 通过，50.8 秒，退出码 0**。包括原图形编辑、中英文、Monaco、真实 KVStore 编译/运行/trace/原生下载、双向定位、死锁与编译失败区分、过期结果和保守因果关系。
- `npm.cmd run build` 和 `npm.cmd run test:smoke`：生产构建、HTML/JS 静态资源与真实 Go health 通过。
- API 覆盖请求限制、本地 Host/Origin、任意命令/路径拒绝、IR/source 冲突、生成源码超限、编译不执行、真实 trace/hash/修订回显。
- runner/trace 覆盖输出截断、取消、timeout、panic、deadlock、真实阻塞、select、名称遮蔽、系统 goroutine、采集辅助事件、Unicode 和精确映射。

环境限制：沙箱内 Windows taskkill 被拒绝使 Playwright 清理挂起。自有子进程对照实验确认原因后，获准在非沙箱环境重跑全部测试并正常退出；未改标准测试配置或系统策略。生产构建还遇到工具用户与仓库所有者不同的 Git 检查，验证时仅通过本次进程的 GIT_CONFIG_* 指定此仓库 safe.directory，未改全局 Git 配置。

保留警告：Monaco 使当前主 JS 约 3.88 MB（gzip 约 1.02 MB），Vite 提示大 chunk，后续可拆包。本轮未在 Windows 运行 go test -race，也未验证 Firefox/Safari。

复现（项目根目录 PowerShell）：

```powershell
$env:Path = "$PWD\.cache\go1.24.13\go\bin;$env:Path"
$env:GOCACHE = "$PWD\.cache\go-build"
$env:PLAYWRIGHT_CHROMIUM_EXECUTABLE = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
npm.cmd test
npm.cmd run test:e2e
npm.cmd run build
npm.cmd run test:smoke
```
