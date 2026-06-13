# Stage 2 设计：多客户端尺寸 + 快照/实时流一致性

状态：SPEC（待批准）。日期：2026-06-13。本机 tmux **3.2a**（已实测）。
关联评审：`docs/reviews/display-alignment-multiclient.md`。前置：Stage 1 + DEBUG 已随 v0.4.9 上线。
本 spec 的关键假设已由对抗式红队 + tmux 实测验证（见 §10）。

---

## 1. 背景与问题（已实测确认）

Termix 把一个 tmux session 的 pane 镜像给浏览器 viewer（PC/手机）。本地 `tmux attach` 终端（host 端，`termix start` 自动 attach）也消费同一个 pane。

实测（v0.4.9 本机）确认两个根因：

1. **尺寸争用**：一个 tmux pane 只有一个尺寸（程序渲染到唯一 grid）。当前协议让**每个浏览器 viewer 用自己的 grid 驱动 `resize-window`** 改全局 pane（`relayclient/client.go` 的 `handleSnapshotRequest`/`handleResizeRequest`），而 `window-size manual`（`tmux/runner.go`）只挡本地 attach、挡不住 relay。实测：host `/dev/pts/15` = 184×40，但 pane 被浏览器撑到 236×57。→ "手机连→host 窗口变小 / PC 连→变大 / 同时连→振荡"。违反 spec §4.4 / §7.3 / line 1900（host 终端尺寸权威；viewer 不得 resize tmux）。

2. **快照↔实时流走偏**：snapshot 用 `capture-pane`（只有可见内容、**不含光标位置/终端模式**），实时用 `pipe-pane`（Claude/Ink 的**光标相对、脏区、不整屏清屏**的增量重绘）。快照应用后浏览器光标停在内容末尾，与 Claude 真实光标不符 → 之后的相对增量重绘落错行。实测：浏览器把第一轮对话+footer 冻在上半屏、第二轮内容贴到最底部，且 footer 帧与真实 pane 不一致。此问题与尺寸无关、即便尺寸稳定也会发生。

### 架构事实（收窄设计空间）
tmux 无法给同一 pane 的不同客户端各自独立尺寸（程序只有一个 PTY grid）。因此唯一可行模型是：**一个权威尺寸 + 各 viewer 本地适配（缩放/平移）**。

---

## 2. 目标 / 非目标

**目标**
- G1：消除 viewer 连接导致 host 原始 tmux 窗口被撑大/缩小。
- G2：PC 与手机以任意顺序同时连接，各自正确渲染，互不破坏。
- G3：单端（PC 或手机）渲染正确，且切换对话/重连后不再 footer 冻住/内容错位。
- G4：合 spec §4.4/§7.3（host 终端尺寸权威；viewer 不 resize pane；viewer 渲染视图并按需适配）。

**非目标**
- 不追求每个 viewer 各自独立的 PTY 尺寸（tmux 不支持，且非产品需求）。
- 不在本阶段切换到 tmux control mode（`-CC`）；在现有 `capture-pane`+`pipe-pane` 模型内解决。
- 不引入 `smallest-wins`（实测会把手机连接时 pane 缩到最小 client，host 不可用）。

---

## 3. 已锁定的设计决策

- **D1 权威尺寸 = host 终端，出生尺寸可显式设定**。有本地 tmux attach 时窗口跟随它（`window-size latest`）；无 attach 时为出生尺寸。**出生尺寸来源优先级**：`termix start --size <cols>x<rows>`（新增，显式）＞ 运行 `termix start` 的终端 tty 尺寸（`hostStdoutWinsize`）＞ 默认 120×40。`--size` 用于 headless / 浏览器优先（与终端解耦，设大一点不 attach 终端即可让浏览器用满）；一旦 attach 终端，`latest` 仍以终端为准（`--size` 只决定出生/无 attach 时的尺寸）。
- **D2 viewer 永不 resize pane**。viewer 的视口变化只影响**本地适配**，不改 tmux。
- **D3 viewer 适配 = 缩小适配 + 双指缩放**。pane 比视口小（PC）→ 原生尺寸 letterbox（不放大、清晰）；pane 比视口大（手机）→ 整体 `transform: scale()` 缩小装下，配双指缩放+平移看细节。`scale = min(1, 视口宽/pane像素宽)`，永不放大。

---

## 4. 架构

### 4.A 尺寸模型（host 权威）— 后端

- **`tmux/runner.go:241`**：StartSession 的 `set-option window-size manual` → **`window-size latest`**。
  - 已实测（tmux 3.2a，§10）：`latest` 让窗口确定性地跟随**最近活动的本地 tmux client**，多 client 不振荡（184→100×29→160×49）；capture-pane/pipe-pane 的 viewer **不是 tmux client、不影响尺寸**；detached 时保持上次尺寸。这正是 D1 所需。**不保留 manual、不分阶段推迟**——manual 反而要 daemon 主动 resize-window 才能跟 host，逻辑更多。
  - 多 host attach（如两个 SSH）下 `latest` 跟最后操作的那个，符合 D1 直觉（非 bug，见 §8 N4）。
- **出生尺寸 + `--size` 旋钮**（`cmd/termix/main.go`）：
  - `parseStartArgs` 新增 `--size <cols>x<rows>`（或 `--cols/--rows`）解析；`runStart` 的出生尺寸取值优先级 = `--size` ＞ `hostStdoutWinsize()`（tty）＞ 0（→ daemon `initialPaneSize` 默认 120×40）。
  - 该尺寸经 `StartSessionRequest.Cols/Rows` → `runner.go new-session -x -y`。`--size` 与 `latest` 协作：它只定出生/无 attach 时尺寸；attach 终端后 `latest` 以终端为准。
- **移除 viewer 驱动的 `resize-window`**（`relayclient/client.go` + `session/manager.go`）：
  - `handleSnapshotRequest`（`client.go:316-318`）：删除 `if req.Cols>0 && req.Rows>0 && c.resizeHandler!=nil { _=c.resizeHandler(...) }`，统一走单次 `snapshotHandler`（line 322 分支）。
  - `handleResizeRequest`（`client.go:334-377`）：删除 line 349 的 `resizeHandler` 调用，并删除其后（line 368+）的 `captureStable`+publish——viewer 视口变化是纯前端 CSS 缩放，不需改 pane、不需新快照。**保留信封解析入口**（守卫 + DEBUG log，line 355-357），收到旧 SPA 的 `client.resize` 时静默忽略尺寸动作（见 §4.D 向后兼容）。
  - `manager.ResizeSession` 保留（供 host 路径/未来），不再由 viewer 触发。
- **host 改尺寸 → 推送（主路径 = `client-resized` 钩子，轮询为 fallback）**：
  - **主**：StartSession 额外 `set-hook -t <session> client-resized 'run-shell "<notify daemon>"'`。已实测：3.2a **有** `client-resized`（`window-resized` 在 3.2a 不存在，3.4 才有）。host attach 终端拉伸 → SIGWINCH → tmux 触发该 client 的 `client-resized` → 通知 daemon。viewer 经 capture-pane 消费不是 client，不触发它。
  - 通知通道：钩子 `run-shell` 在 tmux server 进程里跑，daemon 是独立进程——通过轻量信号（touch 一个 per-session 标记文件 / 写 daemon 的一个 control FIFO / 调 `termix __daemon-notify`）唤醒 daemon。实现时选最简可靠者；如过于别扭则退轮询。
  - **fallback / 兜底**：daemon 侧轮询 `#{window_width}x#{window_height}`（仅在 ≥1 viewer watching 时活跃，间隔 ~500ms）。
  - **去抖 + 比对**（N1/N2/S2）：钩子/轮询只置"脏"标记；daemon debounce ~500ms~1s 后，**先比对实际 window 尺寸是否变化**（`client-resized` 也会在 attach 瞬间触发）再决定推送；推送时用现有 **`captureStable`（poll-until-stable）** 抓帧（host 的 SIGWINCH 重绘是异步的，不能单次 capture）。
  - 推送内容：给所有 watcher 发「新权威 `cols/rows` + 新快照」，复用 `snapshot.ready`（带尺寸）+ `PublishSnapshot` 路径（host-resize 推送**复用当前 generation**，不递增——它不是新一轮 watch）。

> 实施时序（避免半成品破坏现网）：① 先实现并单测 host-size 监测+推送；② 再移除 viewer 驱动的 resize。两步都属 Phase 2a。

### 4.B viewer 采用权威尺寸 + 本地适配 — 前端

- daemon 把**权威 pane 尺寸**放在 `snapshot.ready` 信封（见 §4.D）告诉 viewer。
- **`web/app/src/ui/terminal.ts`**：
  - 新增 `setAuthoritativeGrid(cols, rows)` → `xterm.resize(cols, rows)`。xterm 的内容尺寸**由权威尺寸决定**，不再由 pickGrid 的视口推导值驱动。
  - 适配层：xterm 容器外包一层 wrapper，按 `scale = min(1, viewportW / (cols × cellW))` 做 `transform: scale()`（origin 左上），**永不放大**；wrapper 外层 `overflow:auto`。缩放因子**舍入**（如就近到 0.05 或 0.5 倍）以减小 canvas 亚像素模糊（S4）。
  - `recompute()` / 视口变化（含 Stage 1 的 visualViewport 键盘逻辑）**只重算 scale**，不再调 `setGrid` 改 xterm 尺寸。键盘遮挡与 scale 的几何需一并重算（S4）。
  - pickGrid 降级：保留用于 ① 旧 daemon 回退（见 §4.D）② 可选 hint 上报；不再是内容尺寸来源。
  - DEBUG overlay 增加：`pane WxH（权威）· scale`，便于真机诊断。
- **`web/app/src/bridge/inbound.ts`**：收到带 `cols/rows` 的 `snapshot.ready` → 调 `ui.setAuthoritativeGrid(cols,rows)`。视口变化**不再发改 pane 的 resize**。
- **双指缩放/平移**：优先依赖**浏览器原生** pinch-zoom（页面/容器允许 `touch-action: pan-x pan-y pinch-zoom`），不自造手势与原生缩放打架（S4）；我们的 `transform:scale` 只做"装下"的初始适配，用户在其上原生捏放。
- **坐标映射注意（S4）**：xterm v5.5.0 用 `getBoundingClientRect()`（已含 CSS transform）映射点击→单元格，原则上兼容 scale；但**必须真机实测**：缩放后的网格在手机点击，验证行列正确（只读 viewer 无输入、不受影响；控制态 viewer 需验证）。

### 4.C 快照↔实时流一致性 — 后端 + 前端

- **快照带光标（后端，`tmux/control.go`）**：抽纯函数 **`BuildSnapshot(content []byte, cursorX, cursorY int, visible bool) []byte`**（类比现有 `NormalizeSnapshot`，便于 TDD）：输出 = reset 前缀 `\e[3J\e[2J\e[H` + CRLF 化内容 + 末尾追加光标定位 `\e[{cursorY+1};{cursorX+1}H`。
  - 光标坐标来自 `tmux display-message -p '#{cursor_y} #{cursor_x} #{cursor_flag}'`（实测 3.2a 返回 0-based；CUP 为 `row;col`=`y;x`，**解析顺序须与 format 字符串顺序严格一致**，否则行列互换）。
  - 坐标必须在 **同一次 `captureStable` 收敛后（pane 稳定）** 取，避免 old-size 内容配 new-size 光标错位（M5/R6）。
  - **只恢复光标位置，不无条件恢复可见性**（S1）：仅当 `cursor_flag=0`（隐藏）时追加 `\e[?25l`；绝不无条件下发 `\e[?25h`（靠 Ink 下一帧自设）。
  - **SGR**：内容与光标恢复之间默认**不**插 `\e[0m`（Ink 每帧自带 SGR）；列为 defer-if-broken（N6）。
  - **分块边界**（N5）：若快照分多个 `FrameTypeSnapshotChunk`，光标定位序列必须在**最后一个 chunk 末尾**。
  - **必须 TDD**：单测 `BuildSnapshot` 字节输出；并新增端到端复现测试（snapshot 含光标序列 → xterm 解析 → 后续相对 CUP 落在预期行），在该测试通过前"光标恢复修 footer 冻住"是未验证假设。
- **世代围栏（generation fence，前后端，必须项——race 已实证）**：
  - generation = per-session 单调 `uint64`，session 出生为 0，**每次收到 `session.watch`（新 viewer attach / 重连 re-watch）递增**；host-resize 触发的新快照**复用当前 gen**（非新一轮 watch）。
  - 放在 **`snapshot.ready` 信封 payload**，**不改二进制帧头**（避免 relayproto 协议破坏）。
  - fence 规则（`session/watcher.ts` + `bridge/inbound.ts`）：viewer 收到 `snapshot.ready(gen=N)` 才开始应用该轮；在本轮 `snapshot.ready`+快照二进制帧到达**之前**到的实时输出帧**全部丢弃**；隐含 gen < 当前的帧丢弃（按到达顺序 + 当前 gen 判定，无需帧头携带 gen）。
  - **为何必须**（M4/R2 实证）：relay 先 `forwardEnvelope` 转发 `snapshot.ready` 信封（`server.go:110-116`），daemon 再单独 `PublishSnapshot` 发二进制帧（`client.go:331`）；两者之间到达的 live output 帧会被当前无 gen 逻辑的 `watcher.ts` 直接 apply → 旧输出叠在新快照上 → 错乱。
- **残留**：FIFO/pane 双源在 capture 瞬间的极小重叠窗口，靠 Claude 每轮全量重绘自愈，标记为已知次要项。

### 4.D 协议改动（向后兼容）

- `schemas/ws/*` + `relayproto`（Go）+ `protocol/types.ts`：
  - **`session.snapshot.ready`** 信封新增字段：`cols`、`rows`（权威尺寸，新 daemon **必含**）、`generation`。
  - **不放 `session.joined`**：它由 relay 生成（`server.go:132-134`），relay **不知道 pane 尺寸**，无法注入（S5/R6）。
  - 已确认 relay 原样转发 daemon 信封的额外字段（`server.go:178-181` forwardEnvelope，payload 为 `map[string]any`），故 cols/rows/generation 透传，**relay 无需改**（除非要 relay 暴露 watcher 计数，见 N3）。`protocol/envelope.ts` 解码宽容额外字段。
- **向后兼容 / 版本错配**：
  - **新 SPA + 旧 daemon**（snapshot.ready 无 cols/rows）：SPA 检测不到权威尺寸 → **回退 Stage 1 行为**（pickGrid 驱动 + 继续发 client.resize）。因此新 SPA **必须保留** pickGrid + client.resize 路径作为回退（不能删，只在"收到权威尺寸"时切到采用模式）。
  - **旧 SPA + 新 daemon**：旧 SPA 仍发 client.resize；新 daemon 静默忽略其 resize（S6）→ pane 保持 host 尺寸，旧 SPA 按自己 grid 渲染 → 与 pane 不符、错位。这是**已接受代价**：`ensureDaemon` 强制 **daemon==CLI** 版本（`main.go:444-502`），autoUpdate SW 收敛 SPA，错配窗口短。注意 ensureDaemon **不**管 SPA(浏览器) 版本，唯一 skew 是 SPA vs daemon。
  - daemon + SPA 同版本发布（autoUpdate）。

---

## 5. Spec 合规

- §4.4「local terminal size is authoritative」→ D1（window-size latest 跟随 host attach）。
- §4.4「Android viewport resize must not resize tmux」/ line 1900 → D2（移除 viewer resize）。
- §4.4「renders a view of the current terminal and may scroll if needed」→ D3（采用权威尺寸 + 本地适配）。
- §7.3「if no local terminal is attached, tmux remains at default size until a local attach」→ D1 的"无 attach 用出生/默认尺寸"（实测 latest detached 保持上次尺寸，成立）。

---

## 6. 分阶段实施

- **Phase 2a — 尺寸 + 协议基础设施**
  1. `window-size latest`（+ `client-resized` 钩子，轮询 fallback，含去抖+比对+captureStable）。
  2. 协议加 `cols/rows/generation`；daemon 在 `snapshot.ready` 下发（**generation 字段在 2a 即铺好**）。
  3. host-size 监测+推送（先做、单测）→ 再移除 viewer 驱动 resize。
  4. 前端：`setAuthoritativeGrid` + CSS 缩放适配 + 原生双指缩放 + 旧 daemon 回退。
  - **验证点**：原始 host 窗口不再被 viewer 撑动；PC+手机任意顺序同时连，各自渲染、互不破坏；host 拉伸 → viewer 跟随。
- **Phase 2b — 同步硬化**
  1. 快照带光标恢复（`BuildSnapshot` + TDD 复现）。
  2. generation fence 丢弃逻辑（字段已在 2a 铺好）。
  - **验证点**：切换对话/重连不再 footer 冻住/内容贴底。
  - **裁决**：推荐 2a+2b **一起上**（fence 与光标恢复实现成本都小，且 2a 单独可能仍走偏）；但若 2a 真机测后走偏已消失，2b 可据实测裁剪。**不**把"2a 后或许不需 2b"当默认（解决原 §6 与风险表的矛盾）。

每个 Phase 用 TDD；先各自验证再叠加。

---

## 7. 测试策略

- **Go 单测**
  - `runner_test.go`：StartSession argv 含 `window-size latest`（替换原 manual 断言）；含 `set-hook client-resized`。
  - `main_test.go`：`parseStartArgs` 解析 `--size 220x50` → cols=220,rows=50；优先级 `--size` ＞ tty ＞ 默认；非法 `--size` 格式报错。
  - `client_test.go`：`handleSnapshotRequest`/`handleResizeRequest` **不再调用 resizeHandler**（行为反转，更新现有断言，含 §10 实测过的 client.go:316/349）；`snapshot.ready` 含权威 `cols/rows/generation`。
  - `control_test.go`：`BuildSnapshot(content, x, y, visible)` 纯函数字节输出（reset 前缀 + CRLF + 末尾 `\e[{y+1};{x+1}H`；hidden 时含 `\e[?25l`）。
  - 端到端复现：snapshot 含光标序列 → 后续相对 CUP 落在预期行（证光标恢复修走偏）。
  - 新增 `go/tests` 集成回归：**两个不同尺寸 viewer 同时 watch，pane 尺寸不被任一 viewer 改变**；host-resize → 推送新 cols/rows+快照。
- **Web 单测（vitest）**
  - `terminal.test.ts`：`setAuthoritativeGrid(cols,rows)` → xterm.resize 到该值；scale=min(1, viewportW/(cols×cellW))，不放大；scale 舍入。
  - `inbound.test.ts`：收到 snapshot.ready(cols,rows) → 调 setAuthoritativeGrid；viewport 变化**不再**发改 pane 的 resize；旧 daemon（无 cols/rows）→ 回退 pickGrid+client.resize；generation 落后帧被丢弃（`watcher.ts`）。
- **真机**：DEBUG overlay 读权威尺寸/scale；host 拉伸→viewer 跟随；PC+手机同时连；缩放后点击坐标正确（控制态）。

---

## 8. 风险 / 开放问题

- **N1**：host SIGWINCH→TUI 重绘异步——host-resize 推送必须用 `captureStable`（poll-until-stable），不能单次 capture。（已纳入 §4.A）
- **N2**：`client-resized` 也会在 attach/detach 时触发——daemon 推送前必须比对实际 window 尺寸是否变化。（已纳入 §4.A）
- **N3**：relay 的 registry 私有，无 per-session watcher 计数对外接口。"仅有 viewer 时才轮询/才推"依赖此。**开放项**：是否需新增 daemon 可查询 watcher 数的路径，或先无条件推（有 viewer 才有人收，浪费可接受）。
- **N4**：多 host attach 下 `latest` 跟"最近活动"client（实测），符合 D1，非 bug。
- **N5**：分块快照的光标序列必须绑定到最后一个 chunk 末尾。（已纳入 §4.C）
- **N6**：SGR `\e[0m` 默认不插，defer-if-broken。（已纳入 §4.C）
- **R3 通知通道**：`client-resized` 钩子的 `run-shell` 如何最简可靠地唤醒独立的 daemon 进程（标记文件 / FIFO / `termix __daemon-notify`）——实现期定，过别扭则退轮询。
- **光标恢复充分性**：是否还需恢复 scroll region(DECSTBM)/origin/wrap——先只做光标位置，TDD 复现若仍走偏再加（YAGNI）。
- **前端 scale 与输入法/触摸**：真机实测项。

---

## 9. 不做（YAGNI）
- tmux control mode 重写。
- 每 viewer 独立 PTY / 独立 session-group。
- smallest-wins 自动协商。
- 全量终端状态序列化（仅做光标位置这一最小必要项）。
- 无条件恢复光标可见性 / 无脑插 SGR reset。

---

## 10. 已验证前提（红队 + tmux 3.2a 实测，抛弃 session 上完成，未触碰 termix_*）

1. **`window-size latest` 多 client 确定不振荡**：184→attach 100×30→pane 100×29→attach 160×50→pane 160×49（跟最近 client）；`largest`=160×49；`smallest`=100×29。→ `latest` 是 Stage 2 正确选择，Phase 2a 直接上。
2. **viewer（capture-pane/pipe-pane）不是 tmux client、不影响 window-size**：capture 后 `list-clients` 不变、尺寸不变。D1/D2 基石成立。
3. **detached session 在 `latest` 下保持上次尺寸**（resize 到 120×40 后无 client 仍持 120×40）。对应 §7.3。
4. **`window-resized` 钩子在 3.2a 不存在（`invalid option`），但 `client-resized` 存在且有效（exit=0）**，`after-resize-window` 亦在全局钩子列表。→ host-resize 探测主路径用 `client-resized`，非"只能轮询"。
5. **relay 原样透传 daemon 信封额外字段**（`server.go:178-181`），`protocol/envelope.ts` 解码宽容 → cols/rows/generation 加到 snapshot.ready 即可，relay 无需改；向后兼容（旧端忽略额外字段）成立。
6. **快照 reset 前缀使快照自清屏、幂等**（`control.go`），与追加光标恢复兼容（clear→content→restore-cursor 顺序执行）。
7. **`ensureDaemon` 强制 daemon==CLI 版本**（`main.go:444-502`），唯一 skew 是 SPA vs daemon；配合 autoUpdate，错配窗口短。
