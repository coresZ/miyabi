# Miyabi 重构方案

- 日期：2026-09-19
- 基线：`master / 85228f7`
- 范围：五条工作线。① 全局代理；② 整体结构重构（含冗余清理）；③ 磁力聚合（JavDB + JavBus）；④ javdb-cli 接口补齐评估；⑤ 字幕自动化与播放集成。
- 约束：前端所有样式与动效原样沿用，前端只做结构性调整；预览视频、DMM、第三方图库不在本轮范围；`AUDIT.md`、`AUDIT_RESPONSE.md` 及其临时实验产物随本轮一并删除，已经合并进代码的修复保留。

---

## 目录

1. 目标架构
2. 工作线 A：全局代理
3. 工作线 B：结构重构
4. 工作线 C：磁力聚合
5. 工作线 D：javdb-cli 接口评估
6. 工作线 E：字幕自动化与播放集成
7. 执行顺序与里程碑
8. 验证与回归边界
9. 决定记录

---

## 1. 目标架构

### 1.1 包布局

```
cmd/miyabi/                 main.go 只解析参数和信号；装配移到 internal/app
internal/
  app/                      组合根：配置 → 存储 → 网络 → 服务 → 任务注册 → 路由
  config/                   环境变量 + 运行时常量（超时、限速、缓存尺寸、轮询间隔）
  domain/                   纯模型与错误，无外部依赖
  netx/                     代理管理器、HTTP 客户端工厂（resty / tls-client 两种）
  storage/                  ent 客户端、迁移、原生索引、settings 读写
  tasks/                    任务队列、worker pool、处理器注册表、SSE 总线、typed payload
  drive/                    115 账号与挂载目录状态、SourceSession（原 service/pan_*.go）
  pan/                      115 HTTP client（现有，基本不动）
  javdb/                    JavDB App API client（现有，返回 domain 类型）
  javbus/                   JavBus HTML client（新）
  catalogue/                目录服务：搜索/浏览/详情/标签/媒体，缓存与路由，本地状态投影
  magnet/                   磁力源接口、聚合器、质量推断
  library/                  影片索引、扫描、观看记录（原 library*.go、watch_history.go）
  library/scrape/           刮削、封面、sidecar、元数据快照（原 scrape.go、cover.go、metadata_snapshot.go）
  offline/                  离线下载
  monitor/                  新片监控
  playback/                 播放会话与 HLS 代理
  maintenance/              数据目录统计与缓存清理（原 data.go）
  api/                      gin 路由、bind/respond 助手、错误映射、DTO
  image/ nfo/ codeid/ logging/   不动
```

依赖方向自上而下单向：`api → 业务包 → drive/tasks/catalogue → pan/javdb/javbus/netx → domain`。业务包之间不得互相 import 具体类型，只能通过在 `domain` 或调用方定义的接口交互。

### 1.2 三个核心接口

```go
// drive.Session：一次业务操作期间对 115 的受控访问。签发时捕获账号、挂载目录、
// 授权版本；每次调用前后比对版本；Commit 在持有 drive 提交锁的前提下开事务并复查。
type Session interface {
    Source() domain.LibrarySource
    Version() uint64                                                   // 签发时的授权版本，播放会话据此校验
    List(ctx context.Context, dirID string, offset int) (pan.FilePage, error)
    Info(ctx context.Context, fileID string) (pan.FileInfo, error)
    Read(ctx context.Context, pickCode string, limit int64) ([]byte, error)
    Upload(ctx context.Context, dirID, name string, body []byte) error
    PlayURL(ctx context.Context, pickCode string) ([]pan.PlaySource, error)
    AddOffline(ctx context.Context, magnet string) (string, error)
    RemoveOffline(ctx context.Context, hash string) error
    OfflineTasks(ctx context.Context, page int) (pan.OfflinePage, error) // 只绑定账号，不绑定挂载目录
    Commit(ctx context.Context, fn func(tx *ent.Tx) error) error        // 持提交锁后复查挂载目录与授权版本
    CommitAccount(ctx context.Context, fn func(tx *ent.Tx) error) error // 持提交锁后只复查凭据，离线任务在换目录后仍可落账
}

// tasks.Handler：每种任务由所属包实现并注册，队列不再知道任何领域规则。
type Handler interface {
    Kind() tasks.Kind
    Handle(ctx context.Context, job tasks.Job) error
    // 可选钩子 FinishedHook：在完成事务内执行，返回本次变更影响的修订号，队列提交后发布
    Finished(ctx context.Context, tx *ent.Tx, job tasks.Job, result error) (tasks.Change, error)
}

// magnet.Source：按番号提供磁力。
type Source interface {
    Name() string
    Find(ctx context.Context, ref domain.MovieRef) ([]domain.Magnet, error)
}
```

### 1.3 身份与数据模型

- `movie.javdb_id`、`actor.javdb_id`、`tag.javdb_id` 三列保留原名，NFO 的 `uniqueid type="javdb"` 不变。JavDB 是唯一的目录身份来源，JavBus 只按番号补充。
- `domain.MovieRef{Code, JavDBID}` 是所有增强源的输入。
- `domain.Magnet` 新增 `Sources []string`、`Tags []string`、`Inferred bool`。现有字段和 JSON 名保持不变，前端零破坏。

---

## 2. 工作线 A：全局代理（已完成，2026-09-20）

### 2.1 现状（改造前）

- 只有环境变量 `MIYABI_PROXY`，进程启动时一次性传给 `javdb.Options.Proxy` 与 `pan.Options.Proxy`。
- 四处各自构造 HTTP 客户端：`javdb/transport.go`（tls-client）、`javdb/media.go`（resty）、`pan/client.go`（resty ×2）。运行时不可改，UI 不可见。
- 115 走代理通常适得其反（国内直连更快，且代理出口可能触发风控），JavDB/JavBus 通常必须走代理。所以"全局"应理解为：一个全局代理地址，按目标可开关。

### 2.2 设计（按实现结果修订）

**配置模型**（settings 表，key `network.proxy`）

```json
{ "enabled": true, "url": "http://127.0.0.1:10777" }
```

- 一个开关加一个地址，没有按目标的细分。开启时 JavDB 与 JavBus 全部走代理，115 永远直连。
- `MIYABI_PROXY` 与 `config.Proxy` **彻底删除**，不作为初始种子。理由：115 必须直连，JavDB 也提供直连线路，没有"必须靠环境变量才能启动"的场景；settings 无记录时默认直连。
- 支持 `http://`、`https://`、`socks5://`，可带用户名密码。**不做密码脱敏**：自托管单用户，且已有访问密码门禁，脱敏加"******"回填只增加复杂度。
- 校验失败返回 `domain.E(KindInvalid, ...)`，错误文案为中文，API 错误中间件按 Kind 映射为 400，前端直接展示后端消息，不做字符串匹配。

**`internal/netx`**

```go
// proxy.go
type ProxyConfig struct { Enabled bool; URL string }
func NewProxyManager(ProxyConfig) (*ProxyManager, error)
func Normalize(ProxyConfig) (ProxyConfig, *url.URL, error)   // 校验 + 去空白 + 解析，url 在未开启时为 nil
func (m *ProxyManager) Config() ProxyConfig
func (m *ProxyManager) Resolve() *url.URL                     // 未开启返回 nil，每个请求调用
func (m *ProxyManager) Update(ProxyConfig) error              // 校验、发布、广播（持久化在 service 层）
func (m *ProxyManager) Subscribe() <-chan struct{}
func (m *ProxyManager) Unsubscribe(<-chan struct{})

// client.go：所有上游 HTTP 客户端只能从这里创建
func NewRestyClient(m *ProxyManager, opts RestyOptions) *resty.Client          // JavDB 媒体、（后续）JavBus 详情
func NewDirectRestyClient(opts RestyOptions) *resty.Client                      // 115，永不读代理
func NewFingerprintClient(opts FingerprintOptions) (tlsclient.HttpClient, error) // JavDB API、JavBus；代理在构造时固定
```

- resty：注入 `http.Transport{Proxy: func(*http.Request) (*url.URL, error) { return m.Resolve(), nil }}`，每个请求实时取值，改代理无需重建客户端。
- tls-client：代理只能在构造时指定。`javdb.newTransport(host, proxy *url.URL, options)` 接收已解析的代理；`javdb.Client` 订阅变更后调用 `reinstall()` 重建当前路由的 transport（失败时记 `slog.Warn` 并保留旧 transport），自动路由则再触发一次 `Reselect`。
- `javbus.Probe` / `javdb.Probe` 各自封装在自己的包里，只接收 `*url.URL` 与超时，不依赖 manager。
- `cmd/miyabi/healthcheck.go` 保持不用代理。

**API**

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/settings/network` | 返回 `enabled` 与 `url` |
| PUT | `/api/settings/network` | 校验、保存并广播；JavDB 客户端收到通知后自行重建 transport，自动路由时重新探测 |
| POST | `/api/settings/network/test` | 并发探测 JavDB `/api/v1/startup` 与 JavBus 首页；请求体为候选配置，空请求体则用当前配置；返回各自耗时与错误 |

**前端**：设置页"网络代理"分区，复用 `features/settings/shared.tsx` 布局原语：一个开关、一个地址输入框、一个测试按钮，测试结果以单条 toast 汇总 JavDB / JavBus 的耗时或"不可用"，不展示原始错误文本。JavBus 数据开关留待工作线 C 加入同一分区。不新增样式。

### 2.3 步骤（完成情况）

1. ✅ `internal/netx`：管理器 + 客户端工厂，单测覆盖 Resolve 开关、Update 广播与合并、URL 校验、工厂按请求解析代理。
2. ✅ `pan.New()` 无参数走直连工厂；`javdb.Options.Proxy` 改为 `*netx.ProxyManager`。
3. ✅ `javdb.Client.reinstall()` 订阅变更重建 transport，单测覆盖。
4. ✅ settings 读写、三个端点；校验错误按 `domain.KindInvalid` 映射 400。
5. ✅ 前端分区，探测结果单条 toast 汇总。
6. ✅ 删除 `config.Proxy` 与 `MIYABI_PROXY`（README 中的该变量说明待用户自行更新，AGENTS.md 禁止擅改 README）。

---

## 3. 工作线 B：结构重构

### 3.1 原则

- 行为不变：对外 HTTP 契约、数据库 schema、任务 payload 的 JSON 形状、NFO 输出全部保持。用黄金测试锁定。
- 叶子优先：先抽出被依赖最多、自身依赖最少的部分（错误与模型、任务、drive 会话），再迁业务包。
- 每一步都是一个可独立合并的提交，`go test ./... -race` 全绿。
- 迁移时顺手删冗余，但不做行为优化。行为优化列入 3.7 单独处理。

### 3.2 步骤 B0：安全网（先于一切）

1. 端到端测试 `internal/app/e2e_test.go`：内存假 115（现有 `panStub` 模式）+ 固件 JavDB（现有 `fixtureTransport`），跑"选目录 → 扫描 → 刮削 → 封面 → 上传"，断言 NFO 字节、图片键、`scrape_status`、任务链。
2. 线格式黄金测试：`/api/discover/movies/:id`、`/magnets`、`/api/library/movies`、`/api/tasks`、`/api/offline/tasks`、`/api/monitors` 响应快照进 `internal/api/testdata`。
3. `errorMiddleware` 映射测试、SSE 三类事件测试。
4. 删除 `AUDIT.md`、`AUDIT_RESPONSE.md`、`.tmp/audit-*`。
5. 补 `.gitattributes`（`* text=auto eol=lf`），避免 CRLF 噪音污染重构 diff。

### 3.3 步骤 B1：`domain` 与错误

**模型迁移**

- `internal/javdb/model.go` 中的 `Movie、MovieDetail、MovieReference、Magnet、PreviewImage、Actor、Tag、TagOption、TagCategory、Series、Maker、Director、Zone、EntityType、SearchOptions、BrowseOptions` 迁入 `internal/domain`，JSON tag 不变。
- 不采用类型别名过渡（杜绝脚手架残留），直接一次性全仓原子替换为 `domain` 引用，保持代码整洁。
- `service.DiscoverMovie` 等 DTO 改为内嵌 `domain.Movie`，黄金测试证明输出一致。
- `javdb.Options`、`RouteStatus`、`RouteCandidate`、`APIError`、`HTTPError` 留在 javdb（属于该客户端的运维概念）。

**错误模型**

```go
package domain
type Kind int // Invalid, Unauthorized, NotFound, Conflict, Busy, Upstream, Canceled, Internal
type Error struct { Kind Kind; Message string; Cause error }
func E(kind Kind, message string, cause error) *Error
func (e *Error) Error() string; Unwrap() error; PublicMessage() string
```

- `api/error.go` 只按 `Kind` 映射状态码，响应体只放 `Message`；`Cause` 进日志。sentinel switch 删除。
- 现有 42 处内联中文 `fmt.Errorf` 逐一改为 `domain.E(...)`，英文包装链保留在 `Cause`。
- `pan.apiError` 已有 `PublicMessage()`，映射为 `Kind=Upstream`，115 原文作为 Message。

### 3.4 步骤 B2：`tasks` 包（已完成，2026-09-21，提交 `2613122` 及后续收口）

从 `service/task.go`（471 行）抽出，拆为：

| 文件 | 内容 |
| --- | --- |
| `kind.go` | `type Kind string`，常量 `KindScan/KindScrape/KindCover/KindOffline`；全仓库 8 个文件的字面量替换 |
| `queue.go` | `Claim / Finish / Recover`，`Lock / Unlock` 队列门；队列不含任何领域规则，完成后的修订号由处理器的 `Finished` 钩子返回（`tasks.Change` 位掩码），队列在事务提交后发布 |
| `registry.go` | `Register(Handler)`，`NewHandler(kind, handle, finished)`，`finished` 为 nil 时不实现 `FinishedHook`；pool 从注册表取处理器 |
| `bus.go` | `Subscribe / Notify / Changed(Change) / Revisions`，一处扇出；`Notify*` 便捷方法保留给业务包 |
| `workflow.go` | `List / Info / workflowInfos`（scan+scrape+cover 折叠投影）与窗口函数查询 |
| `payload.go` | `EncodePayload / DecodePayload[T] / SetPayloadField`，`Path*` 常量与 `JSONExtract(column, parts...)` 集中定义 JSON 路径；`database/indexes.go` 与 service 层所有 `sqljson.Path` / `json_extract` 引用这里的常量 |
| `pool.go` | 从 `internal/worker/pool.go` 迁入 |

- ✅ `TaskService.Finish` 中修改 `movie.scrape_status` 的逻辑移到 `ScrapeService.Finished` 钩子；失败返回 `ChangeLibrary|ChangeHistory`，成功返回 `ChangeOffline`；`LibraryService.Finished` 返回 `ChangeOffline`。与原行为逐项等价。
- ✅ `OfflineService` 对未导出 `workflowInfos` 的调用改为公开的 `Workflows(ctx, records)`。
- ✅ `LibrarySource / LibraryDirectory / ScanProgress` 迁入 `domain/source.go`，service 层不留别名。
- ✅ 全仓 `"scan" / "scrape" / "cover" / "offline"` 字面量与裸 JSON 路径替换完毕（`data.go`、`library_scan.go`、`metadata_snapshot.go`、`movie_state.go`、`offline.go`、`scrape.go`）。
- ✅ `PanService.SelectDirectory` 不再持有队列门：B3 改为 `drive` 发布 `MountChanged` 事件，`library` 订阅并调用 `tasks.EnqueueFreshScan`。见 3.5 `events.go`。
- ⏭ `worker/offline.go`、`worker/monitor.go` 两个 ticker 循环合并为 `tasks.RunPeriodic` 列在 3.8，随 B5 / B6 迁包时处理。

### 3.5 步骤 B3：`drive` 包与 `Session`（已完成，2026-09-21，提交 `326f63e` 及后续收口）

原 `service/pan.go、pan_state.go、pan_token.go、pan_directory.go、pan_pagination.go`（776 行）迁入 `internal/drive`：

| 文件 | 内容 |
| --- | --- |
| `drive.go` | `Drive` 结构：凭据、挂载目录、三个版本号、提交锁、`Close` |
| `account.go` | `Account / BeginLogin / LoginStatus / Disconnect` |
| `mount.go` | `Files / SelectDirectory / ClearDirectory`，挂载目录只保留内存快照 + 版本号一处真相；`loadLibrarySource` 从各读路径删除 |
| `token.go` | `withPanToken` 的主动/被动刷新与 singleflight |
| `session.go` | `Open(ctx) (Session, error)`：签发时校验账号（带 60 秒 TTL 缓存，替代每次操作都打 `/open/user/info`），封装 `sourceState → withPanSourceToken → checkScanSource` 三段式与 `commitSource` |
| `pagination.go` | `WalkFilePages / WalkOfflinePages` |
| `events.go` | `MountChanged` 订阅；`SelectDirectory` 改为发布事件，`library` 订阅后调用 `tasks.Service.EnqueueFreshScan`，队列门不再被 drive 持有（从 B2 顺延）。监听者在 drive 持有提交锁期间运行，返回错误则挂载回滚（内存与 settings 一并还原） |

替换点（共十余处）：`library_source.go:29-64`、`library_scan.go:284-301`、`cover.go:218-244`、`play.go:110-136`、`offline.go:189-240`、`scrape.go:66-76` 等。完成后 `ScrapeService`、`PlayService`、`DataService` 对 `library.drive.*` 的 33 处穿透全部消失。

收口（2026-09-21）：`PlayService`、`ScrapeService` 直接持有 `*drive.Drive`，`library.drive.*` 穿透归零；`DiscoverService` 通过构造参数注入 `SourceProvider` 窄接口（B7 的 `catalogue.LocalState` 前身），不再 `SetDrive`；`drive` 不导出任何测试钩子，service 测试经由真实 QR 登录与 `SelectDirectory` 建立夹具；原 `pan_*_test.go` 的 24 个并发用例按新 API 迁入 `internal/drive`，另补挂载回滚、会话失效矩阵、`ValidateSource`、离线分页用例。

### 3.6 步骤 B4 到 B7：业务包迁移

按顺序，每迁一个包提交一次：

**B4 `library` + `library/scrape`**（约 2300 行）

- `library_scan.go`（649 行）拆为：`scan/walker.go`（BFS、分页、`scanPage`）、`scan/identity.go`（`identifyScanVideos`、单 NFO 启发式）、`scan/persist.go`（`processScanPage`、`savePage`）、`scan/reconcile.go`。`Scan()` 从 180 行压到调度骨架。
- `scrape.go`（435 行）拆为：`scrape/scrape.go`（任务处理器）、`scrape/nfo_source.go`（`findNFO / directoryNFO / readNFO`，同时吸收 `library_scan.go:236-263` 的重复 NFO 读取）、`scrape/mapping.go`（`movieNFO / detailNFO / saveMovieMetadata` 三份字段映射合并为一份 `domain.Movie ↔ nfo.Movie ↔ ent` 的双向映射）。
- **番号识别与 NFO 双向容差校验策略（消除硬匹配字典）**：
  - 针对分销商数字前缀（如 `4k688.com@200GANA-3458.mp4` vs `GANA-3458.nfo`）与无码厂商纯日期番号（如 `Carib-060326-001.mp4` vs `060326-001.nfo`）导致刮削被拦截的问题，**杜绝在代码中维护 `prefixAliases` 等静态硬编码字典**。
  - **扫描协同**：在独占单片目录下，已有 NFO 具有更高元数据权威性。当视频名提取出的候选番号与同目录下唯一 NFO 满足亲缘容差时，以 NFO 内标准番号入库建档，避免脏前缀进入数据库。
  - **分级亲缘校验算法（`codeid.IsEquivalent` / 容差比对）**：
    1. 规范化全等：去除标点后大小写不敏感全等；
    2. 核心序列与前缀容差：数字核心序列（如 `3458`、`060326-001`）完全一致的前提下，若一方前缀是另一方前缀的后缀/子集（如 `200GANA` 包含 `GANA`，或厂牌 `CARIB` 与纯日期），判定为同一影片，信任 NFO 标准番号；
    3. 安全拦截：核心数字不一致或前缀毫无关联时严格拦截，防止串片。
  - **模块沉淀**：由 `scan/identity.go` 与 `scrape/nfo_source.go` 共享该纯函数逻辑，入库与刮削双向统一。
- `cover.go`、`metadata_snapshot.go` 迁入 `scrape/`。
- `watch_history.go`、`library.go`、`scan_observations.go` 迁入 `library/`。
- **浏览历史迁表**：`browse.viewed_movies` 目前是 settings 表里一个最多 5000 个 JavDB ID 的 JSON blob（`service/discover_viewed.go`），每次上报整读整写、每次页面加载全量下发。迁 `library` 时改为 `viewed_movie(javdb_id UNIQUE, viewed_at)` 表，`GET /api/discover/viewed` 改为按 `viewed_at` 倒序分页或带 `since` 增量；接口路径与 JSON 形状不变，前端 `browse-history-store.ts` 只需改拉取逻辑。`DiscoverService` 上的 `ViewedMovieIDs / AddViewedMovieIDs` 与 `viewedMu` 随之移出，catalogue 不持有用户状态。
- `library_source.go:13` 与 `pan_directory.go:53-61` 的路径拼接合并为 `drive.DirectoryPath`。

**B5 `offline`**（797 行）拆为 `add.go`（入口与锁）、`submit.go`（115 去重启发式）、`sync.go`（轮询与状态转换 `updateTask / markMissing / completeTask`）、`projection.go`（phase 计算 `submissions`）、`locks.go`（原 `offline_operations.go`）。对 `scanPayload` 的直接构造改为调用 `library.EnqueueTargetedScan(...)`。

**B6 `monitor`、`playback`、`maintenance`**：基本原样迁移；`play.go/play_stream.go` 拆为 `session.go / files.go / proxy.go / playlist.go`。

**B7 `catalogue`**：`discover.go、discover_cache.go、discover_tags.go、movie_state.go` 迁入。`MovieStates` 需要本地库状态，通过 `catalogue.LocalState` 接口由 `library` 实现注入。`DiscoverService.javdb` 改为 `catalogue.Provider` 接口，JavDB 是唯一实现；`Facets()` 暴露 zones、排序、分类槽位供前端后续数据驱动（本轮前端不接）。

**B8 `app` 组合根**：`cmd/miyabi/main.go` 的 `run()` 拆为 `internal/app/app.go`，`New(cfg) (*App, error)`、`Run(ctx)`；任务处理器由各包 `Register`；`internal/service` 目录删除。

### 3.7 步骤 B9：API 层与配置

- `api/helpers.go`：`bindJSON / bindQuery / bindURI` 与 `respond(c, value, err)`、`accepted(c, value, err)`；保留流式与 SSE 特殊路径。约 60 处样板收敛。
- `noStore()` 中间件定义一次（现 `router.go` 三处）。
- `config.Runtime`：收纳 pool 大小、离线轮询 30s、监控 5m、115 限速 2 req/s、超时 35s/45s/2m、播放会话 8h、缓存尺寸与 TTL、`minVideoSize`、视频后缀、sidecar 大小上限。环境变量 `MIYABI_*` 可覆盖，未设置用现值。
- 日志级别校验只留 `logging` 一处。

### 3.8 冗余与死代码清单

迁移时随手处理，每项都有明确位置：

| 位置 | 处理 |
| --- | --- |
| `errPanSourceChanged` vs `library_scan.go:116`、`scrape.go:73` 的同文案字面量 | ✅ 统一为 `drive.ErrSourceChanged`（`domain.Kind=Conflict`）；`ErrMediaDirectoryRequired` 同样只保留 `drive` 一份 |
| `contextLock` 在 `tasks/queue.go`、`drive/drive.go`、`service/lock.go` 三份 | ✅ 收敛为 `internal/syncx.ContextLock` |
| `library_scan.go:236-263` vs `scrape.go:256-291` NFO 读取 | 合并为 `scrape.readNFO` |
| `pan/*` 11 处 `json.Unmarshal + result.err()` 样板 | 抽 `apiRequest[T]`，仿现有 `authRequest[T]` |
| `pan/upload.go:37` ≤128KiB 时两次 SHA1 | 复用 |
| `javdb/transport.go:82-89` 先读全 body 再判状态码 | 先判状态码，非 2xx 只做有上限 drain |
| `javdb` 中 `slog.Warn` 与 `WarnContext` 混用 | 统一 `WarnContext` |
| `javdb/client.go:40` 字段 `selectRoute` 与包级函数同名 | 字段改名 `selector` |
| `pan/play.go:74` 基础设施层中文文案 | 改为 `domain.E` 由上层赋文案 |
| `pan/file.go:47-56` `Count/Size` 未用 `json.Number` | 与同结构其它字段一致 |
| `service/*` 直接用 `slog.Default()` 两处 | 注入 logger |
| `worker/offline.go`、`worker/monitor.go` 两个几乎相同的 ticker 循环 | 合并为 `tasks.RunPeriodic(name, interval, wake, fn)` |
| `internal/ent/enttest` 生成但未使用 | 保留（生成物），不手删 |
| 前端 `api/library.ts:8`、`api/offline.ts:7`、`api/watch-history.ts:5` 反向 import features | toast 移到调用方 `onSuccess/onError`；`watchSessions` 移入 `features/player` |
| 前端三份 `page` 校验器 | `lib/search-schema.ts` 一份 |
| 前端三处 `DiscoverResults` 参数展开 | 组件直接接收 `UseQueryResult` |
| 前端四处 `sameSource` 判断、两处 zone Select、两处确认对话框、两处卡片包装 | 各收敛为一份，DOM 与 class 不变 |
| 前端 `clamp` 手写 5 处 | `lib/utils.ts` 一份，明确 NaN 语义 |
| 前端 `features/tasks/task-progress-state.ts` 与 `scan-status.ts` 两套阶段映射 | 共享阶段定义，文案各自保留 |
| 前端 `lib/watch-progress.ts` 与 `features/player/watch-progress.ts` 撞名 | 后者更名 `watch-progress-writer.ts` |
| 前端 `shadcn` 在 `dependencies` | 移到 `devDependencies` |

### 3.9 前端优化与拆分清单

原则：不改任何 className、动效、骨架屏数量与路由结构；每项以"渲染 DOM 一致"为验收。按优先级：

**结构**

1. `src/api` 变叶子层：`api/library.ts:8`、`api/offline.ts:7` 的 toast 移到调用组件的 `onSuccess/onError`；`api/watch-history.ts:5` 依赖的 `watchSessions` 移入 `features/player`。
2. `features/player/movie-player.tsx`（319 行）拆为 `movie-player.tsx`（数据加载与文件选择）、`playback-player.tsx`（Vidstack 装配）、`use-playback-source.ts`（画质切换、重试、续播 seek 三组 ref 与 state）。`PlaybackPlayer` 现在同时管 5 个 ref、5 个 state、8 个事件。
3. `features/discover/page.tsx`（246 行）把 `CategoryContent / BrowseResults / CategoryFilters` 拆成独立文件；`CategoryFilters` 的 13 个 props 全部来自 store，改为组件内直接读 store。
4. `features/history/page.tsx`（247 行）把多选状态机与确认对话框拆为 `use-history-selection.ts` 与 `history-clear-dialog.tsx`。
5. `features/tasks/task-notifications.tsx` 的 97 行 `useEffect` 抽成纯函数 `diffTaskNotifications(prev, next)` 并补单测；`version: JSON.stringify([...9 字段])` 改为显式比较。
6. `features/settings/pan-directory-dialog.tsx`（215 行）拆 `directory-breadcrumbs.tsx` 与 `directory-list.tsx`。
7. `api/movie-detail-cache.ts` 的 `findCachedMovieCard` 每次快照全量扫描所有 discover 查询，改为维护 `id → 最新卡片` 索引，随 query cache 事件更新。

**去重**

8. 三份 `page` 校验器 → `lib/search-schema.ts`。
9. 三处 `DiscoverResults` 参数展开 → 组件直接接收 `UseQueryResult`。
10. 两处 zone Select、两处确认对话框、两处卡片包装、四处 `sameSource` 判断、五处 `clamp` 各收敛为一份。
11. 三种 entity 形状（`NamedEntity / LibraryEntity / MetadataEntity`）与两种 source scope（`LibrarySource / WatchHistoryScope`）统一为一份类型与转换函数。
12. `MagnetCard` 9 个 props 中 5 个派生自两个查询，卡片自己订阅；配合工作线 C 改为标签驱动。
13. 查询默认项（`retry:false, refetchOnWindowFocus:false`）在各文件手抄，收敛到 `QueryClient` 默认或一个共享工厂。
14. `discoverKeys` 从 `movie-detail-cache.ts` 移回 `discover.ts`；`watchHistoryKeys` 嵌套在 `libraryKeys` 之下的隐式耦合加注释或拆开。

**卫生**

15. `lib/watch-progress.ts` 与 `features/player/watch-progress.ts` 撞名，后者改 `watch-progress-writer.ts`。
16. `task-progress-state.ts` 与 `scan-status.ts` 两套阶段映射共享阶段定义。
17. `shadcn` 移到 `devDependencies`；`ui/card.tsx`、`ui/tabs.tsx` 零引用导出按需清理（`ui/pagination.tsx` 已于 2026-09-21 被 `ListPagination` 全量引用，不再清理；`GoogleCastButton` 是 Vidstack 类型必填项，不能删）。
18. `client.ts` 的 `imageURL` 特判 `/api/library/artwork/` 前缀，改为后端统一返回可直接使用的 URL 后删除。

不在本轮：发现页状态迁 URL、facets 数据驱动、类型生成。这些会改交互或需要装工具，等结构稳定后再议。

---

## 4. 工作线 C：磁力聚合

### 4.1 为什么只要 JavDB + JavBus

两者都是人工维护的聚合站，磁力都经过筛选并带站方标注的"高清/字幕"标签，质量远好于 Sukebei 这类原始索引。JavBus 的收录与 JavDB 有明显互补（JavBus 常先收录 DMM 系新片的磁力，且旧片资源保留更久），合并去重后覆盖面提升明显。两者都以番号为键，不需要身份映射。

### 4.2 `internal/javbus` 客户端

**传输**：`netx.NewFingerprintClient(TargetJavBus)`（Chrome 指纹，JavBus 前面有 Cloudflare，普通 Go client 容易被拦），cookie jar，限速 1 req/s，超时 15s。基础域名可配置，默认 `https://www.javbus.com`，备选镜像列表由用户填写。

**协议**（2026-09-19 已通过本机代理 `127.0.0.1:10777` 实测，固件已存入 `internal/javbus/testdata/`）：

1. 详情页 `GET {base}/{CODE}`。**必须带 `Cookie: dv=1`**，否则无论番号是否存在都 302 到 `/doc/driver-verify` 的问卷页；`existmag=all` 另外附上以显示全部条目。`age=verified` 无效。
2. 从页内脚本提取三个变量：`var gid = 45622804531; var uc = 0; var img = '/pics/cover/83ie_b.jpg';`。
3. 磁力片段 `GET {base}/ajax/uncledatoolsbyajax.php?gid={gid}&lang=zh&img={img}&uc={uc}&floor={1..1000 随机}`，请求头带 `Referer: {base}/{CODE}` 与同一 cookie。实测返回 200，SSIS-001 得到 86 条。
4. 响应是 HTML 片段，若干 `<tr>`：第 1 列 `a[href^=magnet:]` 的 href 含 `xt=urn:btih:<hash>&dn=<名称>`（hash 大小写混杂，需小写归一），同列内 `a.btn-primary` 文本"高清"、`a.btn-warning` 文本"字幕"；第 2 列体积 `2.02GB`（1024 进制）；第 3 列日期 `2025-10-28`。
5. 番号不存在时（带 `dv=1`）返回真实 404，页面标题 `404 Page Not Found! - JavBus`，视为"无结果"而非错误。
6. 无码番号（如 `070125_001`）同一路径可用；FC2、western、anime 直接跳过不请求。
7. 详情页还含 `識別碼 / 發行日期 / 長度 / 導演 / 製作商 / 發行商`、演员 `a.avatar-box[href*=/star/]`、样品图 `.sample-box`，后续做详情补缺时可复用同一份 HTML。

**镜像管理**：JavBus 没有可选镜像，只有 `www.javbus.com` 一个域名，不做端点管理。域名放在 `config.Runtime` 里可被环境变量覆盖即可。

**解析**：用 `golang.org/x/net/html`（已在 go.sum，是间接依赖，需要提升为直接依赖，属于既有模块不算新装；若你希望用 goquery 需要你来安装）。

**缓存**：详情页 HTML 按番号缓存 5 分钟，为后续详情补缺复用。

**开关**：`magnet.javbus.enabled` 默认关闭；没有代理条件的用户保持关闭，聚合器只跑 JavDB，行为与现在完全一致。开关放在设置页"网络"分区，开启时触发一次探测并显示结果。

**测试**：固件三份：`detail_ssis-001.html`（41 KB）、`magnets_ssis-001.html`（79 KB，含高清 26 条、字幕 8 条）、`notfound_zzzz-99999.html`。另需构造 driver-verify 302 与 Cloudflare 挑战页（识别 `Just a moment` 返回 `Kind=Upstream`）两个用例。

### 4.3 `internal/magnet`

```go
type Aggregator struct { sources []Source; timeout time.Duration; logger *slog.Logger }
func (a *Aggregator) Find(ctx, ref domain.MovieRef) ([]domain.Magnet, error)
```

- errgroup 并发，每源独立 `context.WithTimeout`（默认 8s）；单源失败只记 `WarnContext`，全部失败才返回错误。
- 去重：infohash 小写；同 hash 合并 `Sources`（保持 javdb 在前）、`HasSubtitle/HD` 取 OR、`Size` 取非零最大、`Name` 取 JavDB 的、`CreatedAt` 取最早。
- 标签：`Tags` 由站方标注生成（`高清`、`字幕`），外加 `quality.Infer(name)` 补充 `4K`、`无码`、`破解`。推断规则移植 JHS 的 `classifyQuality`：
  - 中字：`(?:[^A-Za-z]|^)FHDC(?:[^A-Za-z]|$)`、`[-_](?:UC|CH?)(?:[^A-Za-z]|$)`、中文关键词表（中字/中文/字幕/繁中/汉化/内嵌/内封/双语…）、含汉字且不含假名。
  - 4K：`(?:[^A-Za-z0-9]|^)(?:4K(?:UHD)?|2160P)(?:[^A-Za-z0-9]|$)`。
  - 无码/破解：`破解|破坏|破壞|无码|無碼`、`\b(?:uncensored|mosaic)\b`、后缀 `-U`、`-UC`。
  - 先剥掉广告括号 `【…APP…】【…夸克…】【…域名…】`。
  - 推断得到而站方未标注的字段标记 `Inferred=true`。
- 排序不变：字幕 > 高清 > 体积 > 文件数；同分时 `Sources` 含 javdb 者在前。
- `catalogue` 现有 `magnets` 缓存（64 条 / 1 分钟）改缓存聚合结果。

### 4.4 接入点

- `GET /api/discover/movies/:id/magnets`：响应每项新增 `sources`、`tags`、`inferred`。
- `POST /api/discover/movies/:id/offline {hash}`：`OfflineService.Add` 校验 hash 时对聚合结果校验，JavBus 独有磁力也能推送。
- `monitor.checkOne`：使用聚合结果；首选含 javdb 的条目，其次含 javbus 的；策略在设置中可调（仅 JavDB / 两者）。
- 设置：`magnet.javbus.enabled`、`magnet.javbus.base_url`、`magnet.javbus.mirrors[]`、`monitor.sources`。
- 前端 `MagnetCard`：现有两个布尔徽章改为遍历 `tags` 渲染，同样式的 `Badge` 组件；每条磁力显示来源徽章（`JavDB`、`JavBus`，两者都有时并列），沿用 `variant="outline"`。卡片布局、间距、动效不变。

工作量约 1 到 1.5 周，依赖工作线 A 与 B1 的 `domain`。

### 4.5 追踪改为订阅（影片 + 演员）

现有 `monitor` 只针对单部未发售影片，状态机 `waiting → added | stale`，入口藏在发现页的一个标签里。改为一等功能：独立路由、两类订阅、批量入库。

**数据模型**（`monitor` 表在 M4 迁移时更名为 `subscription`，字段兼容迁移）

| 字段 | 说明 |
| --- | --- |
| `kind` | `movie` / `actor` |
| `target_id` | JavDB 影片 id 或演员 id，`(kind, target_id)` 唯一 |
| `code / title / cover / release_date` | 影片订阅沿用；演员订阅存 `name / avatar` 复用 `title / cover` |
| `origin_id` | 影片订阅若由演员订阅自动生成，记录来源演员订阅 id |
| `auto_download` | 是否出磁力即推 115；创建时取自设置页默认值，可单条覆盖 |
| `zone` | 演员订阅可限定 censored / uncensored / 不限 |
| `status` | 影片：`waiting / added / stale`；演员：`active / paused / error` |
| `cursor` | 演员订阅游标：上次见到的最新发行日期与影片 id 集合（JSON） |
| `hash / task_id / next_check_at / last_checked_at / checks / error` | 沿用 |

**演员检查**：每天一次，调用现有 `Browse`：`EntityType=actor, EntityID=<id>, Sort=release, Order=desc, Page=1, Limit=40`，与 `cursor` 比对得到新作；每部新作创建一条影片订阅，`origin_id` 指向演员订阅，`auto_download` 继承演员订阅的设置。JavDB 限速 2 req/s，200 位演员每天只有几分钟请求量。

**"入库"的语义**：对一条影片订阅执行入库 = 取聚合磁力，按用户偏好选一条，调用 `offline.Add`，状态转 `added`。没有磁力则保持 `waiting` 并置 `auto_download=true`，出磁力时自动推送。已在库中的影片（`movie-states` 为 `in_library`）不显示入库按钮。

**磁力偏好**（设置页"订阅"分区，全局默认，单条订阅可覆盖）：

| 偏好 | 取值 | 选择规则 |
| --- | --- | --- |
| 字幕 | 优先 / 必须 / 不限 | "必须"时无字幕磁力不入库，保持等待 |
| 高清 | 优先 / 必须 / 不限 | 同上 |
| 无码破解 | 优先 / 必须 / 排除 / 不限 | "排除"时过滤掉 `-U/-UC/无码/破解/Leaked` 标签 |
| 体积上限 | GiB，0 为不限 | 过滤 |

实现为 `magnet.Picker{Preferences}.Pick(magnets) (Magnet, bool)`：先按"必须/排除"过滤，再按"优先"项从高到低打分（字幕、高清、无码各一档，同档按现有排序：体积、文件数、JavDB 来源优先），返回最佳或"无合格磁力"。`monitor.checkOne` 与批量入库都走同一个 Picker，`Inferred` 标签参与打分但权重减半。

**批量入库必须走任务队列**：一次勾选几十部影片如果并发打 115 离线接口会触发风控。`POST /api/subscriptions/enqueue {ids | all: true}` 创建一个 `subscription_batch` 任务，由现有 pool（单 worker）顺序处理，每部之间间隔 1.5 到 3 秒随机，进度与失败明细通过现有 SSE `tasks` 事件推送，前端沿用 `TaskProgress` 与 toast。

**设置页**新增"订阅"分区：影片订阅默认自动推送（默认开）、演员新作默认自动推送（默认关）、演员检查时间（默认每天 04:00）、磁力偏好四项（默认：字幕优先、高清优先、无码不限、体积不限）。

**API**

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/subscriptions?kind=movie\|actor&page=` | 列表，影片项附带 `movie-states` 所需字段 |
| POST | `/api/subscriptions` | `{kind, target_id, auto_download?, zone?}` |
| PATCH | `/api/subscriptions/:id` | 改 `auto_download / zone / status(paused)` |
| DELETE | `/api/subscriptions/:id` | |
| POST | `/api/subscriptions/:id/enqueue` | 单部入库 |
| POST | `/api/subscriptions/enqueue` | `{ids}` 或 `{all: true, kind: movie}` 批量入库，返回 202 与任务 id |
| GET | `/api/subscriptions/actors/:id/feed?page=` | 某位演员的新作动态 |

原 `/api/monitors*` 全部删除，前端一并替换。

**前端**

- `FloatingNav` 新增"订阅"项（`Bell` 图标），路由 `/subscriptions`，`Tabs` 两页：影片订阅、演员订阅。发现页的"监控列表"标签删除。
- 影片订阅页：复用 `MovieGridLayout` 与 `MovieCard`，卡片右上角状态徽章（等待磁力 / 入库中 / 已入库 / 已过期）沿用 `Badge` 变体；头部工具栏复用历史页的选择模式（`selecting / selected Set`、"全选"、"入库选中 (N)"、"一键入库"），一键入库前用现有确认对话框展示将入库数量。单部入库放在卡片 hover 操作区（桌面）与详情页（移动端）。
- 演员订阅页：上方演员横向列表（头像 + 名称 + 新作数 + 自动推送开关 + 暂停/移除），点选某位演员时下方网格只显示其新作，未选中显示全部新作；网格与批量操作和影片订阅页共用同一组件。
- 演员实体页（`/discover/search?kind=actor`）头部加"订阅"按钮；影片详情页现有的追踪按钮改为"订阅"，文案与图标更新，样式不变。
- 所有新组件只组合现有 `Badge / Button / Tabs / Dialog / Avatar / Switch / MovieCard`，不新增 className。

工作量约 1.5 周：后端表迁移与 API 3 天、批量任务 1 天、演员检查 1 天、前端 3 到 4 天。表更名与字段扩展放在 M4 迁 `monitor` 包时一并做，其余在 M6 之后。

---

## 5. 工作线 D：javdb-cli 接口评估

以下都是匿名可用的 App API 端点，miyabi 现有 client 加一个方法即可调用；成本主要在前端呈现。

| 端点 | 用途 | 建议 | 理由 |
| --- | --- | --- | --- |
| `GET /api/v1/rankings?type={zone}&period=daily\|weekly\|monthly` | 影片排行 | **加** | 发现页多一个"排行"标签页，复用 `DiscoverResults`，无新样式 |
| `GET /api/v1/rankings/playback?filter_by=&period=` | 播放排行 | **加** | 同上，作为排行页的一个切换项 |
| `GET /api/v1/rankings/actors?type=daily\|weekly\|monthly` | 演员排行 | 可选 | 需要演员卡片列表，前端有新组件 |
| `GET /api/v1/movies/{id}/reviews?page=&limit=&sort_by=hotly` | 影片评论 | **加** | 详情页折叠区，选片时很有参考价值；一页 20 条不翻页 |
| `GET /api/v1/{actors\|series\|makers\|directors}/{id}` | 实体详情 | **加** | 现在 `/discover/search?kind=actor` 只有作品列表；加头部资料（名称、别名、作品数、头像） |
| `GET /api/v1/lists/related?movie_id=` | 相关合集 | 暂不 | 价值一般，合集页需要新 UI |
| `GET /api/v1/codes/{id}` | 番号前缀实体 | 不加 | 用处很小 |
| `POST /api/v1/sessions`、users/*、reviews 写操作、`movies/top` | 登录与个人状态 | **不加** | miyabi 有自己的观看记录；绑定 JavDB 账号引入封号与隐私风险 |
| `ResolveMovieID` 的"格式等价唯一匹配" | 番号解析 | **采纳** | 分隔符差异（`ABC00123` vs `ABC-123`）可接受，多候选拒绝；比现在严格相等更实用，直接影响刮削命中率 |
| 自动选线 `SelectAutoHost` | 路由 | 不动 | miyabi 现有实现更完整（持久化、手动选择、故障切换） |
| 以图搜番（avscan.cc） | 反搜 | 后议 | 上传截图到第三方，隐私边界需要你决定 |
| 资源下载与 HLS→MP4 重封装 | 预览视频 | 后议 | 属于预览视频范围 |

建议本轮实现"加"的四项加上 `ResolveMovieID` 规则，约 3 到 5 天，其中后端各半天，前端排行标签页与评论折叠区约两天。

---

## 6. 工作线 E：字幕自动化与播放集成

### 6.1 现状与目标

- **现状**：目前 miyabi 仅在扫描时索引视频文件，详情页及播放器无字幕概念；若 115 目录或外部存在字幕，无法被识别与挂载。
- **目标**：实现削刮阶段字幕自动跟随入库；播放器原生支持字幕自动加载、多版本切换、时间轴微调与在线重搜。

### 6.2 字幕来源与确定逻辑

1. **来源策略**：
   - **本地目录优先**：扫描/削刮时检查 115 影片同级目录，若存在同番号或同名的 `.srt / .ass / .vtt` 文件，直接建档入库，不发起外部网络请求。
   - **在线接口兜底**：本地无字幕时，调用迅雷云端字幕接口检索（响应快、免鉴权、中文字幕覆盖率高），备选 SubTitleCat。
2. **确定与打分规则**：
   - **权重打分**：番号完全匹配（最高权重） > 包含完整番号 > 包含关键词；简繁中文字幕加权；`.srt / .ass` 格式加权。得分最高者作为主候选。
   - **版本标识提取**：通过字幕文件名正则嗅探特征词，自动标注版本标签（命中 `uncensored / 无码 / 破解 / 流出` 标注为【无码版】；命中 `extended / 加长 / 完整版` 标注为【加长版】；默认标注为【标准版】）。若本地视频被识别为无码，自动加权优先匹配【无码版】字幕。
3. **编码规范与入库**：
   - 解码链路优先尝试 GBK/GB18030，解析失败回退至 UTF-8 / Big5。
   - 统一转为标准 UTF-8 WebVTT；通过既有的 115 上传通道将规范命名的字幕直传回 115 影片同级目录（保持网盘自闭环），本地存储一份缓存供流媒体秒级输出。

### 6.3 播放器加载与交互

- **自动加载**：`/api/play/files` 响应中附带该影片已入库的字幕轨，播放器启动时原生挂载默认字幕轨。
- **播放器内控制**：
  - 控制栏新增字幕按钮，复用既有 Shadcn 组件（`Dialog`、`Tabs`、`Badge`、`Button`），弹窗风格与播放器全黑磨砂质感统一。
  - **已有字幕页**：支持多轨切换，提供时间轴偏移行微调按键（±0.5s）并可持久化保存。
  - **重新检索页**：允许以番号或自定义词重新搜索候选，展示版本徽章（无码/加长/简中），支持内容预览与一键热加载替换（不中断视频播放）。

### 6.4 涉及范围与 API

- **数据模型**：新增 `Subtitle` 实体，与 `Movie` 关联，记录 115 `pick_code`、语言、版本标签与时间轴偏移。
- **任务集成**：挂在 `scrape` 任务流水线尾部，元数据落库后异步触发。
- **API**：
  - `GET /api/play/subtitles/:id.vtt`（字幕流输出）
  - `GET /api/subtitles/search?code=`（在线候选重搜）
  - `POST /api/subtitles/apply`（下载指定候选并入库上传）
  - `PATCH /api/subtitles/:id/offset`（保存偏移行校准）

---

## 7. 执行顺序与里程碑

| 序 | 里程碑 | 内容 | 估计 | 依赖 |
| --- | --- | --- | --- | --- |
| M0 | 安全网 | B0：e2e、黄金测试、删审计文档、`.gitattributes` | 3 天 | 无 |
| M1 | 全局代理 | 工作线 A 全部（已完成 2026-09-20） | 3 到 4 天 | 无 |
| M2 | 模型与错误 | B1 | 4 天 | M0 |
| M3 | 任务与会话 | B2、B3 | 1.5 周 | M2 |
| M4 | 业务包迁移 | B4 到 B8 | 2.5 周 | M3 |
| M5 | API 与配置收口 | B9、3.8 后端清单 | 4 天 | M4 |
| M6 | 磁力聚合 | 工作线 C | 1 到 1.5 周 | M1、M2 |
| M7 | JavDB 接口补齐 | 工作线 D | 3 到 5 天 | M2 |
| M8 | 前端结构清理 | 3.8 前端清单、3.9 | 1 周 | 可与 M3 到 M5 并行 |
| M9 | 字幕自动化与播放集成 | 工作线 E：削刮入库、GBK 转码、115 同步、播放器字幕管理与微调 | 4 到 5 天 | M2、M5 |

总计约 9 到 10 周单人工作量。M1、M6、M7、M8 都可以和主线并行推进。如果只能串行，顺序就是 M0 → M1 → M2 → M3 → M4 → M5 → M6 → M7 → M8。

---

## 8. 验证与回归边界

- 每个提交：`go test ./... -race`、`go vet`、前端 `node --test`、`tsc`、oxlint、Vite 构建。
- M2 起：黄金 JSON 测试证明所有列出的端点响应逐字节一致。
- M3：`drive.Session` 并发测试沿用现有 `pan_concurrency_test.go` 场景（登出、换目录、令牌刷新中途发生）。✅ 已迁入 `internal/drive/{login,token,mount,pagination}_test.go`，夹具只走真实登录与挂载路径。
- M4：e2e 测试在每个包迁出后重跑；`internal/service` 删除时 e2e 必须仍然通过。
- M6：JavBus 固件测试；聚合器测试覆盖单源超时、单源失败、重复 infohash 合并、`Inferred` 标记、排序稳定性。
- 浏览器行为按项目约定由你验证：设置页网络分区、磁力卡片徽章、排行标签页、评论折叠区。

---

## 9. 决定记录

- `golang.org/x/net/html` 提升为直接依赖。
- 代理：一个开关一个地址；开启时 JavDB 与 JavBus 走代理，115 永远直连。JavBus 无镜像，不做端点管理。
- 代理（2026-09-20）：`MIYABI_PROXY` 彻底删除，不作初始种子；代理密码不脱敏；校验错误中文化并映射 400（哨兵 `netx.ErrInvalidProxy` 已于 2026-09-21 随 B1 删除，改为 `domain.KindInvalid`）。
- 代理开关（2026-09-21）：`Normalize` 在地址为空时把 `Enabled` 归一为 false 并持久化，“开启但无地址”不是合法状态。
- 浏览历史与徽章（2026-09-21，提交 `23c58cc`）：新增"已浏览"功能属于重构窗口内的独立特性，不改变工作线 B 的结构目标。只存 JavDB ID，不存番号。Badge 新增 `library`（紫色，已入库/新入库）与 `frosted`（磨砂，番号/下载中）两个变体，"预览"文案改为"有预览"；这是对"前端样式原样沿用"约束的一次例外，后续 3.9 清单以此为新基线。存储迁表见 B4。
- 错误模型（2026-09-21）：`domain.Error` 不实现自定义 `Is`，哨兵只按指针身份匹配，分类一律走 `domain.IsKind / KindOf`；两者是正交概念，不混用。基础设施层错误类型（`pan.apiError`、`javdb.APIError / HTTPError / networkError`）通过 `DomainKind()` 与 `PublicMessage()` 接入，不在 service 层逐个翻译。
- JavBus 数据默认关闭，设置页可开。
- 磁力按来源加徽章。
- 追踪改为订阅，支持影片与演员；自动推送默认值在设置页由用户选择；独立路由 `/subscriptions` 进 `FloatingNav`；支持单部、多选、一键入库，批量走任务队列。
- 审计文档已删除（提交 `badcfa5`）。
- `config.Runtime` 的环境变量命名先内部集中，对外开放的在实现时逐个写进 README。
- 番号识别与刮削校验（2026-09-20）：针对 `200GANA-3458` 与 `CARIB` 等前缀不一致问题，不引入静态硬匹配字典；在 M4 (B4) 落地“NFO 与文件名双向容差亲缘校验”策略，核心数字一致且前缀包含时自动放行并收敛为 NFO 标准番号。
- 模型迁移（2026-09-20）：M2 (B1) 放弃临时类型别名（type alias）过渡方案，采用全仓一次性原子替换，避免遗留脚手架代码。
- 分页器（2026-09-21，提交 `562f49d`、`58622d6`）：`ListPagination` 从"第 X / Y 页"文字改为 shadcn `PaginationLink / PaginationEllipsis` 页码链接，库页面同时移除"共 N 部影片 · 每页 20 部"文案；这是对"前端样式原样沿用"约束的第二次例外，3.9 清单以此为新基线。`562f49d` 的页码输入框方案已被 `58622d6` 的页码链接替代，最终不存在跳转输入框。页码算法收敛为 `lib/pagination.ts` 的纯函数并配单测：连续窗口固定 3 页（当前页 ±1），首尾页始终可点，总页数不超过 7 时全部列出，省略号不用于只遮一页的情形。链接语义沿用 shadcn 原版：当前页用 `aria-current="page"` 且不可点，禁用态用 `aria-disabled` 加 `pointer-events-none`；上一页/下一页保持仓库既有的原生 `Button`。传入 `totalPages` 的页面（库、观看历史）渲染完整页码，未传的页面（发现页，JavDB 无总数）只渲染当前页占位。
- B3 收口（2026-09-21）：挂载不再与扫描入队同处一个事务，改为 drive 先持久化并切换版本、再在提交锁内同步发布 `MountChanged`；任一监听者返回错误即回滚内存与 settings，对外等价于原来的原子性。监听者约束写进 `SubscribeMount` 注释：不得经由 drive 开会话或提交，否则死锁。挂载触发的扫描只复用 `Queued` 状态（`EnqueueFreshScan`），`Running` 的扫描可能是旧挂载签发的，不能替新挂载；手动重扫仍走 `EnqueueScan` 复用 `Queued+Running`。`drive` 不再导出 `MountSource / SetClient / BumpAuthorization / AuthorizationVersion` 四个只为测试存在的方法，测试改走真实登录与 `SelectDirectory`。`326f63e` 顺手去掉的 `artworkOrigin` json 标签已还原，cover 任务 payload 形状与 B1 之前一致。
- 任务引擎（2026-09-21）：`tasks.Queue` 不包含领域规则。完成后要发布哪个修订号由处理器的 `Finished` 钩子以 `tasks.Change` 位掩码返回，队列在事务提交后统一发布；没有钩子的处理器不触发任何修订。B2 迁出时不引入类型别名，`service` 层直接引用 `tasks.*` 与 `domain.*`。
