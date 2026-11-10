# Notes 桌面版方案：MyGo + 本地 JSON Vault + WebDAV / SMB 同步

状态：方案定稿，P0–P2 与 P1 的原生窗口已实现；P3 未做（偏差与未做项见 §12）；日期 2026-11-10。

## 0. 一页结论

- **界面**：主窗口用 MyGo 的系统 WebView 跑现有 React 界面，编辑器、数据库、关系图原样保留，只换 `src/platform/` 的存储层（约 200 行）。
- **后端**：Go 负责全部后端能力，包括 Vault 读写、落盘、文件监听、附件、索引、同步和凭据。分层照 Kero / GodGo，不搬 Kero 的 daemon、PTY 和会话。
- **Vault 格式**：一页一个 JSON 文件，和现在的记录模型一一对应，转换零损失。附件存成真实文件。
- **远端**：WebDAV / SMB 都采用“本地工作副本 + 同步引擎”。同一页的冲突按记录（块）做三方合并。
- **窗口外观**：
  - macOS 标题栏和内容融合：隐藏标题栏，系统红绿灯嵌进侧栏顶栏那一行。
  - Windows / Linux 隐藏系统按钮，按 Kero / GodGo 的尺寸在页面里自绘最小化、最大化、关闭。
- **性能**：所有记录常驻 Go 内存，写入是内存改动加防抖落盘，前端按增量刷新。以前每写一次就全量重读的开销会去掉。

## 1. 参考对象各拿什么

| 来源 | 拿 | 不拿 |
|---|---|---|
| notes（本仓库） | 全部 React 界面、记录模型、merge-patch 语义 | IndexedDB、BroadcastChannel |
| MyGo（用 0.8.x，本机缓存 v0.8.3） | WebView 窗口、`Bind` + 生成的 TS 客户端、`Event[T]`、`Protocol.Handle`、`WatchFiles`、`Secrets`、`plugins/sqlite`、`plugins/updater`、打包签名 | 不用 `ui` 重写编辑器 |
| Kero（egoist/kero，MIT） | 分层方式：`internal/` 下的 settings / service / remote 不碰 UI；侧栏和设置窗口的层次；Windows 自绘控制栏的尺寸和图标 | kerod daemon、PTY、会话、分屏 |
| GodGo（OpenSpring/GodGo） | 已改编自 Kero 的 `frontend/chrome.go`（`framed`、`windowControls`）、`settings_view.go`、`backend/settings`、i18n、发布工作流、`reuse.md` 规则 | 它的原生侧栏主页（主窗口换成 WebView） |

GodGo 钉在 MyGo 0.6，这里直接上 0.8.x。拷过来的 GodGo 壳代码在同一次改动里跟着升级。

## 2. 仓库结构

本仓库直接变成桌面应用，React 工程移进 `web/`：

```
main.go                      只调 app.Run()
mygo.json                    名称、标识、版本、更新公钥（发布前 mygo keygen 替换）
web/                         现有 React 工程（src/、index.html、vite.config.ts、package.json）
frontend/                    MyGo 窗口层：主窗口(WebView)、Vault 选择窗与设置窗(原生 ui)、菜单
  chrome.go                  从 GodGo 拷：framed()、windowControls()，注明改编自 Kero
  settings_view.go           从 GodGo 拷：设置窗口
  vaults_view.go             Vault 选择窗
internal/
  settings/                  应用设置 JSON（沿用 GodGo backend/settings）
  vault/                     打开/关闭 Vault、内存记录模型、Write、防抖原子落盘、外部修改认领
  index/                     SQLite：全文搜索、反链、标题 → id（P1 后半，需要时再上）
  storage/                   FS 接口：local / webdav / smb
  sync/                      同步清单、三方比较与合并、回收站
  service/                   mygo.Bind 的服务：Vault、Files、Sync、Window
resources/                   字体、图标、文案 zh-Hans / en、界面规范
```

规则沿用 GodGo 的 `reuse.md`：
- `internal/` 不导入 `mygo/ui`，测试不开窗口。
- 只写这一期用得到的，不预建扩展点。
- 用户可见文案放进两份 locale 文件。

浏览器版只留作开发调试：`vite dev` 时继续用 IndexedDB 实现，不再单独发布。

## 3. Vault 格式（JSON）

```
MyVault/
├── pages/
│   ├── <page_id>.json           # 页面 + 它的全部块
│   └── ...
├── attachments/
│   └── <page_id>/<file_id>-<name>.<ext>
└── .notes/
    ├── vault.json               # 格式版本、Vault id、创建时间
    ├── workspace.json           # workspace_settings：preferences、roots
    └── history/<page_id>/<version_id>.json
```

单页文件，字段与现在的 `PageRecord` / `BlockRecord` 完全一致，只换了外层结构：

```json
{
  "format": 1,
  "page": { "id": "p_8f2k", "kind": "document", "parent_id": "p_root", "title_content": [...], "icon": "📘", ... },
  "blocks": {
    "b_19ac": { "kind": "text", "order": 1, "content": [...] },
    "b_19ad": { "kind": "todo", "order": 2, "checked": false, "content": [...] }
  }
}
```

要点：

- **文件名用页面 id，不用标题。** 改标题不会产生文件改名，远端也就不会出现“删一个、建一个”。层级关系靠 `parent_id`，不靠文件夹嵌套。
- 块按 id 存在对象里，不用数组。这样同步合并、diff 和 git 都按块对齐。
- 写出时键排序固定、缩进 2 格，同样内容每次写出的字节相同，减少无意义的同步。
- 数据库的行本身就是 `kind` 为页面的子页面（`row_order`），各自一个文件。视图和属性定义在数据库页面的 `page.database` 里。
- 历史版本放在 `.notes/history`，不进 `pages/`。保留规则沿用 `schema.ts`：每页 20 个版本、30 天、10MB。
- 附件存原始二进制，不转 base64。页面里的引用（`path`、`name`、`size`、`width`、`height`、`preview`）不变，只是 `path` 现在指向 Vault 里的相对路径。
- “导出为 Markdown”作为单向导出功能放在 P3，不进入格式本身。

## 4. Go 服务与前端接缝

### 4.1 绑定的服务（生成 TS 客户端）

```go
type Vault struct{ /* 当前窗口的 Vault */ }

func (v *Vault) Snapshot(ctx context.Context) (Snapshot, error)            // 打开时一次拿全量记录
func (v *Vault) Write(ctx context.Context, w RecordWrite) (Ack, error)      // put / patch / delete，返回合并后的记录
func (v *Vault) WriteFile(ctx context.Context, path string, b []byte, mediaType string) error
func (v *Vault) ImportBackup(ctx context.Context, backupJSON string) (ImportReport, error)

var VaultChanged = mygo.Event[Changes]("vault:changed") // 外部修改、同步下载、另一窗口的写入：只带变化的记录
var SyncState    = mygo.Event[SyncStatus]("sync:state")  // saved / saving / offline / refused，对上前端已有类型
```

附件读取：`mygo.Protocol.Handle("vault", …)` 用 `mygo.FileServer` 提供文件，支持 Range 请求。`<img src="vault://localhost/attachments/...">` 会边读边传，不在内存里攒 Blob。Linux 上的视频走回环 HTTP（MyGo 文档里写明的 WebKitGTK 限制）。

### 4.2 前端改动

| 文件 | 改动 |
|---|---|
| `platform/storage.ts` | 拆出 `RecordStore` 小接口：`snapshot()`、`write()`、`onChange()`。IndexedDB 版保留给 `vite dev`；新增 `store-mygo.ts` 调 Go |
| `platform/plane.ts` | 写入后不再 `refresh()` 全量重读，改为用 `Write` 的返回值和 `VaultChanged` 的增量更新 `stored` |
| `platform/local-files.ts` | `fileUrl` 直接返回 `vault://` URL，`writeFile` 调 `Vault.WriteFile` |
| `platform/backup.ts` | “恢复备份”改成“导入浏览器版备份到当前 Vault” |
| `components/BackupControls.tsx`、`Sidebar.tsx` 文案 | “stored in this browser”之类改为 Vault 语义，并显示 Vault 名 |
| 新增 `app/WindowChrome.tsx` + CSS | 标题栏融合与 Windows 控制栏（见 §6） |

启动时用 `isMyGo()` 选择实现，桌面版构建只带 Go 的那份。

## 5. 写入路径与性能

```
键入 → WorkspaceDocument 生成 RecordWrite → 界面立即显示（pending 投影，不等 IPC）
     → Vault.Write (IPC <1ms) → Go 内存 mergePatch → 返回合并后的记录 → 标记该页 dirty
     → 防抖 500ms（同一页连续输入只合成一次） → 写 pages/<id>.json.tmp → fsync → rename
```

- **内存模型**：打开 Vault 时把 `pages/*.json` 全部读进 `map[pageID]*pageFile`，后续读都不碰磁盘。
- **落盘**：
  - 按页防抖，只重写被改的页面。
  - 窗口失焦、关闭、应用退出时立即刷盘。
  - 最坏情况是崩溃丢掉最后不到 500ms 的输入。要做到零丢失再加追加写的日志，这一期不做。
- **自己写的文件不当外部修改**：`WatchFiles` 收到事件后先比内容 hash，hash 等于自己刚写的就忽略。
- **外部修改**（用户用别的工具改了文件、Syncthing / iCloud 同步进来）：重读该页，与内存按记录合并，然后推 `VaultChanged`。
- **启动**：Go 解析几千页、几十 MB JSON 在百毫秒级。`Snapshot` 一次 IPC 交给前端，和现在从 IndexedDB `getAll()` 是同一个量级。P0 用真实备份实测，到上万页再考虑懒加载正文。
- **已知不动的开销**：`Plane.project()` 每次写入都复制全部记录。这个开销现在就有，换存储后不变；笔记规模大到卡顿时再单独优化。

## 6. 窗口与标题栏

一个 Vault 对应一个主窗口（WebView），另有 Vault 选择窗和设置窗口（MyGo 原生 `ui`，从 GodGo 拷）。原生的两扇窗直接用 GodGo 的 `framed()` 和 `windowControls()`，已经满足本节要求。下面只说主窗口，也就是 WebView 页面怎么做到同样的效果。

### 6.1 共同规则

- 外框交给系统：不用 `Frameless`，保留系统的缩放边、阴影、圆角，也不自己画窗口描边。规则同 GodGo `ui-spec.md` 的“窗口边框”一节。
- 拖拽区域：`.topbar` 和 `.sidebar__head` 设 `--app-region: drag`。按钮、面包屑、输入框会自动保持可点，不需要逐个声明。双击拖拽区等于双击标题栏。
- 标题栏高度沿用 notes 现有的 `--topbar-h: 38px`。侧栏顶栏和页面顶栏在同一行，这一行就是标题栏，不再额外加一条。

### 6.2 macOS：标题栏融合

```go
mygo.NewWindow(mygo.WindowOptions{
	URL:                  "/",
	TitleBarStyle:        mygo.TitleBarHiddenInset,
	TitleBarHeight:       38,
	TrafficLightPosition: &mygo.Point{X: 14, Y: 12}, // 灯的中心落在 y=19，即 38 高这一行的中线；P0 实机校准
	Transparent:          true,                       // 只在打开“半透明侧栏”时
	Vibrancy:             mygo.VibrancySidebar,       // 同上
	StateKey:             "vault:" + vaultID,
})
```

- 用系统红绿灯，不自绘也不隐藏，位置用 `TrafficLightPosition`，做法同 GodGo。
- 页面通过 MyGo 注入的 CSS 变量避让红绿灯：
  ```css
  .sidebar__head { padding-left: max(8px, var(--mygo-titlebar-inset-left, 0px)); }
  .app-shell--railed .topbar {
    padding-left: max(10px, calc(var(--mygo-titlebar-inset-left, 0px) - var(--sidebar-rail) + 8px));
  }
  ```
  侧栏展开（216px）时，红绿灯落在侧栏顶栏左侧，视图切换的标签右移让开。侧栏收起成 46px 的窄栏时，红绿灯会跨进页面顶栏，所以面包屑也跟着右移，不被灯压住。
- 全屏时这些变量自动变成 `0px`，红绿灯随菜单栏收起，内容顶到边。
- 开启“半透明侧栏”时，侧栏背景设成透明，透出 `VibrancySidebar` 材质；页面区仍用 `--paper` 不透明背景。这个选项默认开启，设置里可以关掉。
- 失焦窗口：用 `onWindowActive` 给 `<html>` 加 `is-inactive` 类，侧栏文字和图标变淡，和系统应用一致。

### 6.3 Windows：自绘控制栏（参考 Kero / GodGo）

```go
o.TitleBarStyle     = mygo.TitleBarHidden
o.HideWindowButtons = true   // 隐藏系统按钮，保留缩放边（含顶边）、阴影、圆角
o.AutoHideMenuBar   = true   // Alt / F10 仍可打开菜单，快捷键照常
o.Vibrancy          = mygo.VibrancySidebar // Win11 22H2+ 为 Mica；关掉半透明时不设
```

React 侧新增 `WindowControls`，尺寸、图标、交互逐项照搬 GodGo `frontend/chrome.go` 的 `windowControls`（它改编自 Kero）：

| 项 | 值 |
|---|---|
| 位置 | 窗口右上角固定层，`position: fixed; top: 6px; right: 7px; z-index` 高于侧边预览，垂直居中于 38 高的标题行 |
| 顺序 | 最小化、最大化 / 还原、关闭 |
| 按钮 | 每颗 28×26，圆角 6，间距 2，整组左边距 6 |
| 图标 | 16×16（关闭 14×14），描边 1.35，圆头圆角；SVG 路径与 GodGo `minimizeIcon` / `maximizeIcon` / `restoreIcon` / 关闭图标相同 |
| 颜色 | 常态 `--ink-3`；悬停底色 `--fill-hover`、文字 `--ink`；按下底色再淡一级；关闭悬停和按下用危险色、白色图标 |
| 行为 | 调 `mygo-runtime` 的 `currentWindow.minimize()`、`toggleMaximize()`、`close()`；关闭的是当前窗口，不是退出应用 |
| 最大化状态 | Go 在窗口最大化 / 还原时推 `window:maximized` 事件，页面据此切换图标和提示文字 |
| 避让 | 定义 `--window-controls-w: 94px`（6 + 28×3 + 2×2），`.topbar` 和侧边预览头部的 `padding-right` 加上它；macOS 上为 0 |
| 提示 | 三颗按钮都有 tooltip 和 `aria-label`，文案进 locale |

已知取舍：HTML 自绘的最大化按钮悬停时不会弹出 Windows 11 的贴靠布局，因为 MyGo 只给原生 `ui` 的 `MaximizeButton()` 接了这个功能。`Win+Z` 和拖到屏幕边缘贴靠仍然可用。如果以后一定要贴靠布局，就改回 `TitleBarHidden` 不隐藏系统按钮的方案，并按 `--mygo-titlebar-inset-right` 避让。这个改动在 P3 评估，不阻塞前面的工作。

### 6.4 Linux

和 Windows 一样：隐藏系统按钮，页面自绘，与 GodGo 一致，不画红绿灯。Wayland 下由 GTK 补阴影和缩放边，不需要额外处理。顺序先固定为“最小化、最大化、关闭”放在右侧；以后要跟随桌面的按钮布局，再读 MyGo 的 `WindowButtons`。

### 6.5 菜单与快捷键

- 应用菜单：
  - 文件：新建 Vault、打开 Vault、打开最近、立即同步、在 Finder / 资源管理器中显示 Vault
  - 编辑：系统标准项
  - 视图：切换侧栏、切换深色
- 编辑器内的快捷键仍由 React 处理。`CmdOrCtrl+,` 打开原生设置窗口，替代页面里的设置弹窗。

## 7. WebDAV / SMB 同步

### 7.1 结构

- 本地 Vault（普通文件夹）不经过同步引擎，直接用 `storage/local` + `WatchFiles`。用户用 iCloud、Syncthing 同步这个文件夹也能用。
- 远端 Vault 在 `App.Path(PathUserData)/vaults/<vault_id>/` 下放一份本地工作副本。编辑器只读写这份副本，`sync` 引擎负责它和远端之间的往返。
- 库：
  - WebDAV 用 `github.com/emersion/go-webdav`（MIT）
  - SMB2/3 用 `github.com/hirochachacha/go-smb2`（BSD-2）
  - 都是纯 Go，`CGO_ENABLED=0` 不受影响
- 密码存 `mygo.Secrets`（macOS 钥匙串、Windows 凭据管理器、Linux libsecret），设置 JSON 里只记地址和用户名。

### 7.2 一轮同步

1. **列远端**：WebDAV 先试 `PROPFIND Depth: infinity`，服务端不允许就逐层列；SMB 递归 `ReadDir`。每个文件拿到 `etag`（没有就用 `mtime + size`）。
2. **对清单**：清单存在应用数据目录的 SQLite 里，不放进 Vault。每个路径记录 `base_hash`、`local_hash`、`remote_tag`。
3. **按下表处理**：

| 本地 | 远端 | 动作 |
|---|---|---|
| 变 | 未变 | 先上传到 `.<name>.tmp-<device>`，再 `MOVE` / rename 覆盖正式文件 |
| 未变 | 变 | 下载，更新内存，推 `VaultChanged` |
| 变 | 变（`pages/*.json`） | **记录级三方合并**：以 `base` 为底，`page` 的字段和每个块分别比较。不同块、不同字段都保留；同一字段两边都改了，以本地为准，远端那一版整页存进 `.notes/history`，在历史面板里能找回（块记录没有 `updated_at`，不按时间比）。合并结果写本地后上传 |
| 变 | 变（附件或其他文件） | 保留两份，另一份命名为 `名字 (冲突 设备名 2026-11-10).ext` |
| 删 | 未变 | 远端移到 `.trash/<日期>/`，不硬删 |
| 未变 | 删 | 本地移到 Vault 的 `.trash/` |

   `base` 版本取自工作副本旁边的 `.base/` 目录，只给 `pages/*.json` 存一份，用来做三方合并。

4. **触发**：本地写入后防抖 3 秒；定时轮询，默认 5 分钟，可调；窗口获得焦点；菜单里的“立即同步”。
5. **状态**：通过 `SyncState` 映射到前端已有的 `SyncStatus`，顶栏现成的保存指示直接显示同步状态。

### 7.3 兼容要点

- 坚果云有请求频率限制：遇到 429 / 503 指数退避，并且列目录尽量一次完成。群晖、Nextcloud、Alist 正常使用 ETag。
- SMB 用 NTLM 认证，地址形如 `smb://host/share/path`。mtime 精度按 2 秒容差比较。
- 文件名只有 id，天然避开了 Windows / SMB 的非法字符和大小写问题。附件名在写入时清洗。

## 8. 迁移

1. 浏览器版：设置 › 下载备份，得到现有的 `open-notes` JSON。
2. 桌面版：新建 Vault 后，导入浏览器备份。Go 端解析 `records`：按 `blocks` / `versions` 的 `<长度>_<页面id>_<…>` id 把块和历史版本归到对应页面，`files` 的 base64 写回 `attachments/`，`workspace_settings` 写入 `.notes/workspace.json`。
3. 导入报告列出页面数、块数、附件数，以及无法归属的记录（预期为 0）。

## 9. 分期与验收

| 期 | 内容 | 验收 |
|---|---|---|
| **P0 骨架** | MyGo 0.8 Web 工程；React 移进 `web/`；`Vault.Snapshot/Write` + 防抖原子落盘；`vault://` 附件；`plane.ts` 改增量；macOS 融合标题栏；Windows 自绘控制栏 | `go tool mygo dev` 打开后能新建、编辑、加图片，重启后都在；导入现有备份后实测启动耗时和打字流畅度；Windows 上三颗按钮和顶边缩放可用 |
| **P1 本地 Vault** | Vault 选择窗与最近列表（原生）；设置窗口（从 GodGo 拷，加 Vault / 同步页）；`WatchFiles` 外部修改认领；历史写入 `.notes/history`；回收站 | 外部工具改了 json，应用里即时看到且不丢本地正在输入的内容；多开两个 Vault 窗口互不干扰 |
| **P2 远端同步** | `storage/webdav`、`storage/smb`、`sync` 引擎、`Secrets`、连接测试、同步日志 | 两台机器对同一 WebDAV 交替离线编辑同一页的不同块，合并后两边内容一致、无丢失；SMB 同样；拔网后能编辑，联网后自动补齐 |
| **P3 打磨** | 搜索 / 反链改走 SQLite 索引（规模需要时）；导出 Markdown；签名、公证、自动更新；评估 Windows 贴靠布局 | 签名包可以自动更新 |
| 可选 | 参考 Kero 的 CLI（`notes new / append / search`），给 agent 用 | — |

## 10. 测试

- Go：
  - `vault` 用临时目录测往返、防抖合并、崩溃后 `.tmp` 残留的清理
  - `sync` 用内存 FS + 回环的 `golang.org/x/net/webdav` 服务端跑完整的三方合并表
  - SMB 在 CI 里起 Samba 容器
  - 运行 `CGO_ENABLED=0 go test ./...`、`go tool mygo vet ./...`
- 前端：保留 vitest。`storage.test.ts` 改成 `RecordStore` 的契约测试，IndexedDB 和 Go 两个实现共用同一组用例。
- 界面：设置窗和 Vault 选择窗用 `ui.NewTester` 点一遍用户路径。WebView 主窗口的标题栏需要在三个平台上实机截图核对；没有实机打开过，就不写“已验证”。

## 11. 风险

| 风险 | 缓解 |
|---|---|
| 大 Vault 首次加载和 `project()` 全量复制 | P0 用真实数据测；超阈值再做正文懒加载和投影增量化 |
| WebDAV 服务端差异（没有 ETag、禁用 `Depth: infinity`、限流） | 能力探测 + 退化路径 + 退避 |
| 同一字段两边都改时，远端的修改不会出现在正文里 | 远端那一版整页存进历史，可以从历史面板找回；同步日志里列出发生过冲突的页面 |
| Windows 自绘最大化没有贴靠布局 | 已列为已知取舍，P3 评估改回系统按钮 |
| MyGo 0.6 → 0.8 的壳代码升级 | 只拷 `chrome.go`、`settings_view.go`、`backend/settings` 这几个文件，拷过来时就按 0.8 编译通过 |

## 12. 实现状态（2026-11-10）

本节记录实际做到的范围，以及与上文的偏差。

**已实现并有测试或实机操作验证**

- P0：`web/` 迁入；`internal/vault`（记录模型、一页一文件、防抖原子落盘、附件、备份导入、外部修改认领）；`frontend/` 的 `Notebooks` 服务；`vault://<vault id>/files/…` 附件协议；`web/src/platform/` 的 `RecordStore`（`store-mygo.ts` / `store-browser.ts`）与增量 `plane.ts`；macOS 融合标题栏；Windows / Linux 自绘控制栏（`WindowChrome.tsx`，原生窗口同样用 `frontend/chrome.go`）。共享契约用例 `testdata/store-contract.json` 同时由 Go 和浏览器存储执行。
- P1：保险库选择窗（原生，最近列表 + 新建 / 打开文件夹）；设置窗（原生，通用 / 外观 / 保险库 / 同步四页，⌘, 打开）；每个 Vault 一个窗口；最近列表（File › Open Recent）；`.notes/history` 写入；侧栏底部显示 Vault 名；View 菜单的切换侧栏、切换深色。
- P2：`internal/storage`（WebDAV、SMB、本地目录；429 / 503 指数退避；WebDAV 优先一次 infinite-depth PROPFIND，被拒时逐层列目录）；`internal/sync`（清单、按表处理、页面三方合并、冲突副本、`.trash` 与恢复）；同步日志 `sync-log.json`（最近 200 条，设置 › 同步 显示）；冲突与断连的提示；密码进系统钥匙串（`mygo.Secrets`）；同步状态接入顶栏保存指示；连接窗口（File › Connect to a Remote Folder…）。
- P1 / 外观：macOS 半透明侧栏，默认开启，设置里可关（`OpaqueSidebar`）。

**与上文的偏差**

- 清单存为原子写入的 JSON 文件，不是 SQLite。MyGo 的 `plugins/sqlite` 通过 purego 加载系统库，不满足纯 Go 的要求。
- `vault://` 的主机名用 Vault id（`vault://v_xxx/files/…`），不是 `localhost`：协议处理器拿不到发起请求的窗口，多个 Vault 窗口需要靠主机名区分。
- 附件在磁盘上放在 `attachments/`，记录里仍是 `files/…` 路径，两者按前缀一一对应。
- 远端版本的整页存入 `.notes/conflicts/`，没有写进 `.notes/history`。历史面板需要规范化的 `NotePage`（行内内容），这一转换只在前端有。
- 设置窗没有整体复制 GodGo 的 `settings_view.go`：GodGo 的设置页含更新、工作流等本应用没有的页面，且依赖其自己的设置后端和 i18n。这里用 MyGo `ui` 按同样的布局和窗口外壳（`framed`、自绘控制栏）重写了通用 / 外观 / 保险库 / 同步四页。

**未做或未验证**

- P3：搜索 / 反链的 SQLite 索引、Markdown 导出、签名、公证、自动更新（需要证书和更新公钥）、Windows 贴靠布局评估、可选的 CLI。
- 同步轮询间隔仍固定为 5 分钟，没有做成可调。
- 实机验证范围：本机 macOS 上用点击验证了保险库选择、新建页面、外部改文件即时显示、设置开关及持久化、外观切换、侧栏和深浅色切换、同步页的立即同步 / 断开 / 恢复、WebDAV 上传与冲突合并。未验证：Windows 与 Linux 上的窗口和自绘按钮；坚果云等真实 WebDAV 服务；SMB 服务器；连接窗口的表单输入（键盘文本输入在本环境里不稳定，没有完成）；半透明材质的视觉效果（设置已应用，但未确认材质显示）。
