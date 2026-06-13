# 设计评审：Termix 显示错位 / 多客户端尺寸争用 (Design Review: SPA Display Garble & Multi-Client Sizing)

状态：评审 (REVIEW ONLY)。本文不写代码，只定位根因、区分"已确认 / 部分确认"，并给出分阶段修复路线。所有论断附 `file:line` 锚点。

诊断结论一句话：**单客户端的错位是时序/测量问题（多数已被 v0.4.8 缓解），但"手机变小/PC 变大/同时连断裂"是架构问题——一个共享 tmux pane 只能有一个尺寸，而当前协议让每个 viewer 的 viewport 都去 resize 这个全局 pane，且快照被广播给所有 viewer。这不是再打一个补丁能解决的，需要 per-viewer 独立尺寸。**

---

## 1. 端到端尺寸协商链路 (End-to-end dimension flow)

一次"viewer 打开 → 看到画面"的尺寸数据流，从浏览器视口一路追到 tmux 再回到 xterm：

1. **浏览器视口 → pickGrid → cols/rows**
   `containerSize()` 用 `clientWidth/clientHeight`，回退到 `getBoundingClientRect()` 再到 `window.innerWidth/innerHeight`（`web/app/src/ui/terminal.ts:30-36`）。`pickGrid(w,h)` 用**硬编码**单元格尺寸算格子：`cellW = FONT_SIZE(13) * 0.6 = 7.8px`，`cellH = 13 * 1.2 = 15.6px`，`cols = max(80, floor((w-2)/7.8))`，`rows = max(20, floor(h/15.6))`（`terminal.ts:4-7, 22-28`）。**无上限**（v0.4.8 去掉了旧的 120×40 cap），只有 80×20 下限（`terminal.ts:9-15`）。这里没有任何对 xterm 实际渲染单元格的测量（无 FitAddon / CharMeasure，`terminal.ts:57-68` 直接 `term.open` 后无回测）。

2. **xterm 初始化 → bridge 取初值**
   `mountTerminal` 用算出的 cols/rows 构造 xterm（`terminal.ts:60-67`）。bridge 通过 `cols()/rows()` 读到 xterm 当前格子数（`terminal.ts:49-50, 112-113`）作为初始尺寸。

3. **`session.watch` 携带 cols/rows → relay**
   viewer 在 WS 打开时发 `session.watch{session_id, cols, rows}`（`bridge/inbound.ts:98-102`）。这是 v0.4.7/`3209b40` 的关键改动：把尺寸放到 watch 上，避免"watch 前发 client.resize 被 isWatching 拒绝"的握手 bug。

4. **relay 转发 → snapshot.req**
   relay 在 `TypeSessionWatch` 分支里 `addWatcher`，然后把**每个** viewer 的 cols/rows 塞进一个新的 `TypeSessionSnapshotReq` 转给 daemon（`go/internal/relay/server.go:131, 143-153`）。注意：这里没有"是否首个 viewer / 是否已有尺寸"的判断——**每次 watch 都生成一次 snapshot.req**。

5. **daemon resize tmux → 等待 → capture**
   `handleSnapshotRequest`：若 `req.Cols>0 && req.Rows>0` 就**无条件** `resizeHandler()`（→ `ResizeSession` → `tmux resize-window`），然后 `waitForPaneRedraw()`（默认 120ms），再 `capture-pane`（`go/internal/relayclient/client.go:259-264`）。`ResizeWindow` 是一条全局 `resize-window -t -x -y`，无去重无合并（`go/internal/tmux/runner.go:172-184`）。

6. **快照交付 → SPA**
   daemon 先发 `snapshot.ready` 文本信封（触发 SPA `xterm.reset()`），再 `PublishSnapshot` 二进制帧（`client.go:268-272`）。relay 的 `forwardBinary` 把 `FrameTypeSnapshotChunk` **广播给该 session 的所有 watcher**（`server.go:196-199`）。

7. **SPA 渲染 + 实时流**
   SPA 在 `snapshot.ready` 上 `reset()`（`inbound.ts:135-137`），在二进制帧上 `cfg.ui.write()`（`session/watcher.ts:14-18`）。实时 `pipe-pane` 输出（type-1）与快照（type-3）走**同一条 WS、同一个 `cfg.write()`**，无 epoch/seq 守卫（`watcher.ts:14-18`，`client.go:109,128` 把 seq 硬编码为 1）。

8. **后续 resize 回路**
   xterm `ResizeObserver` → `recompute()`（300ms debounce）→ `setGrid` + `window.requestResize`（`terminal.ts:80-100`）→ `client.resize` 信封（`inbound.ts:252-262`）→ relay 转给 daemon（`server.go:154-166`，受 `isWatching` 守卫）→ `handleResizeRequest` 镜像 snapshot 尾部（resize→wait→capture→ready→publish，`client.go:275-313`）。

**链路里每个会断的地方：**
- **(1) 单元格测量假设**：7.8/15.6px 是硬编码，跨字体/DPR/WebView 不一定成立（PARTIAL，见 §2）。
- **(1)/(8) 视口测量盲区**：`containerSize` 不读 `visualViewport`，手机软键盘"pan 模式"下不触发 resize（PARTIAL，见 §2）。
- **(4)/(5) 每个 viewer 都 resize 全局 pane + 广播给所有人**：架构核心缺陷（CONFIRMED，见 §3）。
- **(5) resize→capture 时序竞态**：v0.4.8 用 120ms 缓解（CONFIRMED 机制，缓解非根治，见 §2/§6）。
- **(7) 快照与实时流交织、无 \e[2J、无 epoch**：PARTIAL（见 §2）。

---

## 2. 单客户端错乱根因 (Single-client garble — PC & phone)

单 viewer（PC 或手机）独占时仍可能错乱的**已确认**机制：

### 2.1 resize→capture 时序竞态【CONFIRMED — 已被 v0.4.8 缓解，非根治】
`tmux resize-window` **同步**更新 pane 的 cell array，但 pane 内 TUI 的 SIGWINCH→重绘是**异步**的（~16–60ms）。旧代码 resize 后立即 `capture-pane`，抓到"新尺寸 + 旧布局"，SPA 渲染出错位的快照；实时 `pipe-pane` 又在其上叠加重绘而不清屏，于是光标落错行、回显落错列。
- 证据：`client.go:256-258` 注释明确写了这个竞态；`client.go:259-262`（snapshot 路径）与 `client.go:303`（resize 路径）插入 `waitForPaneRedraw`；`client.go:41-48` 定义 `defaultPaneRedrawDelay = 120ms`。
- 为何重要：这是 PC 和手机首次进入/旋屏/键盘弹起时"第一屏错位、按一下键或 F5 自愈"的直接原因。
- **缺陷仍在**：120ms 是固定启发值。`client.go:41-47` 注释自承"覆盖两三个 React/Ink 渲染帧带余量"。若 TUI（Claude Code / Ink）重绘 >120ms（慢机、负载、大数据帧），仍会抓到"新尺寸+旧布局"。这是 PARTIAL：机制确认，但 120ms 不保证。

### 2.2 快照无前导 \e[2J + 与实时流交织【PARTIAL — 次要】
`capture-pane -p -e`（`go/internal/tmux/control.go:25-27`）产出的是裸 cell dump（CRLF 归一化，`control.go:39-42`），**不带前导清屏**。SPA 依赖收到 `snapshot.ready` 时的 `xterm.reset()`（RIS）先清屏。但 `snapshot.ready`（文本）与快照二进制是**两次独立 WS 写**（`client.go:268-272`），且实时 `pipe-pane`（type-1）由另一个 goroutine 持续推送（`session/manager.go` 的 `streamOutput`）——三者在 SPA 侧的 `onText`/`onBinary` 两个 handler 上可交织。daemon 侧的 `c.mu` 只串行化 WS 写，不保证 reset/snapshot/output 的语义顺序。
- 证据：`inbound.ts:135-137`（reset 在文本 handler）vs `watcher.ts:14-18`（write 在二进制 handler）；`watcher.ts` 对 type-1 与 type-3 一视同仁，无 seq/epoch 守卫；`client.go:109,128` seq 恒为 1。
- 为何是次要：v0.4.8 的 `paneRedrawDelay` 让快照内容本身变正确后，全屏快照基本能覆盖交织进来的增量输出。这是真实但二级的风险（PC/手机都可能命中，概率低）。

### 2.3 单元格度量与硬编码不符【PARTIAL — 主要影响手机，目前够用】
`pickGrid` 假设 cellW=7.8px、cellH=15.6px（`terminal.ts:4-7,22-28`），但 xterm 实际单元格取决于浏览器字体渲染（font-family、CJK fallback、系统字体缩放、WebView 厂商、DPR）。若实测单元格是 8.0px，按 7.8px 多算的列数会让 xterm 右侧溢出（黑边/折行）。代码无任何 open 后回测（`terminal.ts:57-68,108-124`）。
- 为何归 PARTIAL 而非根因：v0.4.8 去 cap + 120ms 后显示已基本正确，说明 7.8px 近似在当前条件够用（`terminal.test.ts` 锁定了 1280px→163 列、3840px→492 列的预期）。这是手机端"黑边/右侧错位"的潜在但未确认放大器，需运行期实测（见 §6）。

### 2.4 手机软键盘高度盲区【PARTIAL — phone-specific】
`useKeyboardOffset()` 监听 `visualViewport` resize 算出 `keyboardOffset`（`web/app/src/hooks/useViewport.ts:6-28`），作为 `paddingBottom` 加到 `.terminal-page`（`pages/terminal.tsx:134`）；`.terminal-page` 是 `flex/100dvh/border-box`，`#terminal` 是 `flex:1`（`theme/styles.css` ~976-982, 1027-1034），所以**在"resize 模式"浏览器里 padding 变化会缩小 `#terminal`，ResizeObserver 正常触发**（`terminal.ts:94-97`）——这条路径是对的。
- **真正的盲区**：在"pan 模式"移动浏览器（部分 Android 默认、旧 Safari）里软键盘弹出时 `visualViewport.height` **不缩**、页面改为滚动平移。于是 `visualViewport` resize 不触发 → `keyboardOffset` 不变 → padding 不加 → `#terminal` 尺寸不变 → ResizeObserver 不触发 → xterm 仍按全屏格子数，键盘遮住底部 → 光标/回显错位。
- 证据：`containerSize()`（`terminal.ts:30-36`）回退链里**没有** `visualViewport.height`；`useViewport.ts:6-28` 只在 `visualViewport` 缩小时才更新。外加 300ms debounce（`terminal.ts:20`）会有一段遮挡窗口。
- 修正后的机制：不是"ResizeObserver 可能不触发"，而是"在 pan 模式下 `visualViewport` 高度根本不变，导致整条 resize 链不被触发"。

**PC 专属 vs 手机专属小结：**
- **PC**：主要是 2.1（首屏 resize→capture 竞态，已缓解）；2.3 在非标准缩放下可能命中。旧的黑边（grid cap）已由 v0.4.8 修掉（C1，CONFIRMED）。
- **手机**：2.4（pan 模式键盘盲区）是手机独有；2.3（CJK/字体缩放导致单元格偏差）在手机更易命中；2.1 同样存在。

---

## 3. 多客户端窗口争用根因 (Multi-client window-size contention)

这是用户"手机连→原窗口变小 / PC 连→变大 / 同时连→断裂"的**已确认架构根因**。

### 3.1 今天的 tmux attach 模型
- **一个 session、一个全局 pane**：daemon 给每个 sessionID 建**一个**共享 tmux session（`runner.go` 的 `new-session`），所有远程 viewer 通过 relay 看**同一个** pane；本地终端也可 `tmux attach-session -t termix_<id>` 接到同一个 pane。
- **`window-size manual`**：StartSession 后设 `set-option window-size manual`（`runner.go:238-242`）。它的作用仅是**挡住本地 host `tmux attach` 把 pane 撑成 host 终端尺寸**（注释 `runner.go:232-237`）。它**不挡 relay 驱动的 `resize-window`**——daemon 的每次 `ResizeWindow`（`runner.go:172-184`）都直接改全局 pane 尺寸。

### 3.2 为什么强制单一共享尺寸
链路 §1 第 4–6 步已证：
- relay 对**每个** `session.watch` 都转发一次带该 viewer cols/rows 的 `snapshot.req`（`server.go:143-153`），无"首个 viewer / 已有尺寸"判断。
- daemon **无条件** resize 全局 pane 到该 viewer 的尺寸（`client.go:259-260`），无 per-viewer 状态、无合并、无 per-session 串行锁（`registry.go` 的 `watchers` 是 `map[string]map[*peer]struct{}`，**不存 cols/rows**；`manager.go` 的 `ResizeSession` 直接调 `ResizeWindow`）。
- 抓到的**单一快照**被 `forwardBinary` **广播给所有 watcher**（`server.go:196-199`），快照头里只有 `session_id/seq/is_last`，**无 watcher_id**，无法定向。

后果（last-writer-wins + 广播错配）：
- 手机（80×20）后连 → pane 被 resize 成 80×20 → 80×20 快照广播给 PC → **PC 看到内容"变小"**（A3/B2，CONFIRMED）。
- PC（245×51）后连 → pane 被 resize 成 245×51 → 245×51 快照广播给手机 → **手机 80 列容不下、折行/右黑边"变大"**（B3，CONFIRMED）。
- 同时连 → 两条 watch 按到达顺序处理，**最后到的赢**。注意：这是**确定性、与到达顺序相关的 last-writer-wins，不是数据竞态**——对抗式验证否定了"race condition"这一措辞（relay-fanout C1 = REFUTED），但正因为它是确定性的，反而**更稳定可复现**。后果：pane 卡在后到 viewer 的尺寸，另一 viewer 收到错配尺寸的快照、且实时流按旧列宽布局写进新列宽 → 错乱（由 §3.2 的 A3/A4/B2/B3 共同 CONFIRMED）。

### 3.3 本地原始终端是否也在抢尺寸
**是，但方向不同**。`window-size manual` 让本地 attach **不能**把 pane 撑回它自己的尺寸；而 relay 端的 viewer resize **能**改 pane。所以：若本地终端在 120×40，手机连上把 pane 压到 80×20，本地终端看到的 TUI 被夹在 80×20（变窄/折行），且**手机断开后没有任何恢复逻辑**——`removePeer`（`registry.go`）只清 watcher，不存原尺寸、不 restore，pane 卡在 80×20，本地终端只能手动 `tmux resize-window` 救回（A3，PARTIAL：机制真实，但"本地被夹"是次场景，主症状是远程 viewer 互相错配）。

### 3.4 与 spec 的冲突（设计意图 vs 实现）
spec §4.4 / §7.3 明确："本地附着终端尺寸是权威的"、"Android viewport resize 必须**不**改 tmux 尺寸"、"若无本地终端附着，tmux 保持默认尺寸直到本地 attach"。当前代码做的是**相反**的：每个远程 viewer 的 viewport 都被当成强制 resize 指令。这是 spec 违背，不只是 bug（A1/A2/D1@spec-lens，CONFIRMED）。

---

## 4. 设计层面的结论 (Architectural verdict)

**已经打过 3+ 个对症补丁，问题仍在 → 这是架构问题，不是再打一个补丁。**

修复历史（来自 `docs/PROGRESS.md` 与 git log）：
1. **v0.4.4 / `46a86c9` + `49a3cad`**：握手加 `client.resize`-先-发，并在 `snapshot.ready` 上 `xterm.reset()` 防快照堆叠。
2. **v0.4.7 / `3209b40`**：把 cols/rows 移到 `session.watch` 上（修握手 `isWatching` 拒绝、保证 capture 前 resize）。
3. **v0.4.8 / `aadee69`**：去掉 `pickGrid` 的 120×40 cap（修黑边）+ 加 `paneRedrawDelay(120ms)` + `handleResizeRequest` 补发快照（修 resize→capture 竞态）。

这三轮都在**单 viewer 的时序/测量层**打转，全部回避了核心：**一个共享 pane 只能有一个尺寸，而协议让 N 个 viewer 各自 resize 它、又把单一快照广播给所有人。** 多客户端症状在每一轮后都复现，正符合"对症修复反复失败 → 升级到架构"的判据。

**缺失的设计：per-viewer 独立尺寸。** 候选架构（与当前代码对比）：
- **(a) 每 viewer 一条独立 tmux session、共享同一组 window**（tmux `new-session -t <group>` session-group / `link-window`）：各 client attach 各自 session，`aggressive-resize on` 让每个 client 看自己尺寸、共享底层 window 内容。最贴近 spec"多 viewer"，但 tmux session-group 对 pane 尺寸的隔离有限制，需验证。
- **(b) 服务端单一 canonical pane + per-viewer 虚拟视口**：pane 锁一个尺寸（首个 viewer 或本地 attach），daemon 为每个 viewer 渲染/裁剪/缩放出各自尺寸的快照与增量。复杂度最高，但最干净、最符合"viewer 不改 pane"。
- **(c) window-size=manual + 每 client 独立虚拟尺寸**：在 (b) 基础上由客户端 xterm 自己 fit/scroll，pane 不随 viewer 变。
- 当前代码 = **(无)**：单一全局 pane + last-writer-wins + 广播，零 per-viewer 状态。

---

## 5. 修复路线 (Fix roadmap, staged)

> 评审建议，不在此实现。按依赖排序。

### Stage 1：让单个 client 完美渲染（最小正确修复）

| # | 层 | 改动 | 风险 | 不能解决 |
|---|----|------|------|----------|
| 1.1 | SPA (`terminal.ts:30-36`) | `containerSize` 在 pan 模式下也能拿到真实可视高度：把 `visualViewport.height`（减去 `offsetTop`）纳入高度回退链，或对 `visualViewport` 的 resize/scroll 也触发 `recompute` | 低 | 多客户端争用 (§3) | 
| 1.2 | SPA (`terminal.ts:57-124`) | open 后**回测** xterm 实际单元格尺寸（measure 一个字符元素），用实测值算 cols/rows，替代硬编码 7.8/15.6 | 低-中（需跨浏览器验证） | 多客户端；时序竞态 |
| 1.3 | daemon (`client.go:41-48,159-170`) | 把 `paneRedrawDelay` 从固定 120ms 改为"capture→对比→稳定即停"的轮询，或可配置 + 记录实测重绘耗时 | 中（慢 TUI 下增延迟） | 多客户端 |
| 1.4 | daemon/tmux (`control.go:25-44`) | 快照 payload 前置 `\e[2J\e[H`（防御性清屏，配合 SPA reset 双保险）；评估补 `-J` 对折行/尾随空格的影响 | 低 | 多客户端 |

Stage 1 完成判据：PC **或** 手机单独连接，首屏无黑边、无错位、键盘弹起后光标对位。

### Stage 2：让 PC + 手机同时连接也正确（架构改动）

| # | 层 | 改动 | 风险 | 不能解决 |
|---|----|------|------|----------|
| 2.1 | 协议 (`schemas/ws/*`, `protocol/types.ts:64-66`, `relayproto/frame.go`) | 给 `session.watch`/`snapshot.req`/snapshot 帧加 `watcher_id`（或 request_id），打通 viewer↔snapshot 的对应关系 | 中（协议演进，需向后兼容旧 viewer） | — |
| 2.2 | relay (`server.go:143-199`, `registry.go`) | (a) `addWatcher` 存 per-viewer cols/rows；(b) snapshot 帧按 `watcher_id` **定向回发**给请求者，停止 `forwardBinary` 无差别广播 | 中 | — |
| 2.3 | daemon (`client.go:259-313`, `manager.go`) | 停止"每个 viewer 都 resize 全局 pane"。落地 §4 的 (a)/(b)/(c) 之一：最小可行是 **per-viewer 虚拟视口**——pane 锁 canonical 尺寸，按 viewer 尺寸裁剪/适配快照；并给 `ResizeSession` 加 per-session 串行锁防并发 resize | 高（核心改动） | — |
| 2.4 | daemon/relay | 遵循 spec §4.4：本地 attach 存在时 viewer resize 不改 pane（`tmux list-clients` 探测本地 client）；记录原 pane 尺寸，末位 viewer 断开后 restore | 中 | — |

Stage 2 完成判据：PC 与手机以任意顺序同时连接，各自按自身视口正确渲染，互不影响；本地终端尺寸不被远程 viewer 破坏。

**依赖关系**：2.1 是 2.2 的前提；2.2/2.3 共同实现 per-viewer 隔离；2.4 收尾 spec 合规。Stage 1 与 Stage 2 可并行启动，但 Stage 1 应先合入以止血单 viewer 体验。

---

## 6. 待验证的开放问题 (Open questions / to instrument)

需要**运行期证据**才能从 PARTIAL 升级为 CONFIRMED / 排除的项：

1. **手机真实单元格尺寸（验证 2.3）**：在目标手机浏览器里 open 后实测 xterm 单个 cell 的 px 宽高，与 7.8/15.6 对比。
   - 仪器：在 `mountTerminal` 后注入临时测量元素或读 xterm 内部 `_core._renderService.dimensions`，打日志到 console / 上报。

2. **pan 模式 vs resize 模式（验证 2.4）**：在目标手机上软键盘弹起时记录 `window.visualViewport.height`、`window.innerHeight`、`#terminal.clientHeight` 三者变化。
   - 仪器：临时 `visualViewport.addEventListener('resize'/'scroll', log)` + `ResizeObserver` 回调打点。

3. **tmux 版本与默认 window-size（验证 §3 假设）**：生产/客户机上 `tmux -V`、`tmux show-options -g window-size`、`tmux show-options -t termix_<id> window-size`，确认 `manual` 是否真的生效、`aggressive-resize` 默认值。
   - 命令：`tmux -V`；`tmux show-options -t <session> window-size aggressive-resize`。

4. **120ms 是否够（验证 2.1 残留）**：测 Claude Code / Codex / Opencode 在 resize 后 SIGWINCH→可见重绘的 P95/P99 耗时。
   - 仪器：daemon 侧 resize 后多次 `capture-pane` 直到内容稳定，记录稳定耗时分布。

5. **多 viewer 并发顺序（验证 §3 振荡）**：PC+手机同时连接时，daemon 侧 readLoop 实际处理两个 `snapshot.req` 的顺序与间隔；是否落在彼此的 120ms 窗口内。
   - 仪器：`handleSnapshotRequest` 入口/出口打时间戳 + viewer 来源日志。

6. **是否有本地 attach 在场（验证 §3.3）**：`tmux list-clients -t termix_<id>` 在 viewer 连接时的输出，确认本地终端是否同时附着、被夹尺寸。

7. **快照/实时流交织实证（验证 2.2）**：抓 SPA 侧 `onText`(snapshot.ready) 与 `onBinary`(type-1 / type-3) 的到达时序，确认是否真有 output 落在 reset 与 snapshot 之间。

8. **多客户端回归测试缺口**：当前 `go/tests/` 无"两个不同尺寸 viewer 同时 watch 同一 session"的用例（`relay_integration_test.go` 只验证两条 snapshot.req 都到达 daemon，未验证尺寸正确性）。Stage 2 需补此测试作为防回归。
