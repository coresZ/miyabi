# Miyabi 重构方案

- 日期：2026-09-19；最近更新 2026-09-22
- 基线：`master / 85228f7`；当前进度基线 `b105941`
- 范围：五条工作线。① 全局代理；② 整体结构重构（含冗余清理）；③ 磁力聚合（JavDB + JavBus）；④ javdb-cli 接口补齐评估；⑤ 字幕自动化与播放集成。
- 约束：前端所有样式与动效原样沿用，前端只做结构性调整（已记录两次例外，见第 9 节）；预览视频、DMM、第三方图库不在本轮范围。
- 进度：A、B0 到 B4 已完成；B5 到 B9、C、D、E 未开始；前端 3.9 与主线并行，已完成分页收敛。

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

### 1.1 包布局（✅ 已就位，⏳ 待迁）

```
cmd/miyabi/                 ⏳ main.go 仍是组合根，B8 迁入 internal/app
internal/
  app/                      ⏳ B8：配置 → 存储 → 网络 → 服务 → 任务注册 → 路由
  config/                   ✅ 环境变量；config.Runtime 待 B9
  domain/                   ✅ 纯模型与错误（B1）
  netx/                     ✅ 代理管理器、HTTP 客户端工厂（A）
  database/                 ✅ ent 客户端、迁移、原生索引（方案原名 storage，沿用现名不改）
  syncx/                    ✅ ContextLock（B3 顺带收敛）
  tasks/                    ✅ 队列、pool、处理器注册表、SSE 总线、typed payload（B2）
  drive/                    ✅ 115 账号、挂载目录、Session（B3）
  pan/ javdb/               ✅ 现有客户端
  javbus/                   仅 probe.go；完整客户端属工作线 C
  library/                  ✅ 影片索引、观看记录、已浏览（B4）
  library/scan/             ✅ walker / identity / persist / reconcile（B4）
  library/scrape/           ✅ scrape / nfo_source / mapping / cover / snapshot（B4）
  catalogue/                ✅ B7（已完成，2026-09-22）
  magnet/                   ⏳ 工作线 C
  offline/                  ✅ B5（已完成，2026-09-22）
  monitor/ playback/ maintenance/   ✅ B6（已完成，2026-09-22）
  worker/                   ✅ 随 B5/B6 并入 tasks.RunPeriodic，目录已删除
  service/                  过渡目录，B8 删除；现余 access_gate、network、setting

  api/                      gin 路由、错误映射、DTO；bind/respond 助手待 B9
  image/ nfo/ codeid/ logging/   不动（codeid 新增 IsEquivalent）
```

依赖方向自上而下单向：`api → 业务包 → drive/tasks/catalogue → pan/javdb/javbus/netx → domain`。业务包之间不得互相 import 具体类型，只能通过在 `domain` 或调用方定义的接口交互。`library/scan → library/scrape` 是同一限界上下文内的子包共享，允许；反向不得出现。

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
    Commit(ctx context.Context, fn func(tx *ent.Tx) error) error        // 持提交锁后复查挂载目录与授权版本
    CommitAccount(ctx context.Context, fn func(tx *ent.Tx) error) error // 持提交锁后只复查凭据，离线任务在换目录后仍可落账

    PlayURL(ctx context.Context, pickCode string) ([]pan.PlaySource, error)
    AddOffline(ctx context.Context, magnet string) (string, error)
    RemoveOffline(ctx context.Context, hash string) error
    OfflineTasks(ctx context.Context, page int) (pan.OfflinePage, error) // 只绑定账号，不绑定挂载目录
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

- **配置**：settings 表 key `network.proxy`，`{ "enabled": bool, "url": string }`。一个开关一个地址；开启时 JavDB 与 JavBus 走代理，115 永远直连。支持 `http:// https:// socks5://` 带用户名密码，不脱敏。`MIYABI_PROXY` 与 `config.Proxy` 已删除，settings 无记录时直连。
- **`internal/netx`**：`ProxyManager`（`Config / Resolve / Update / Subscribe`）与三个客户端工厂 `NewRestyClient`（按请求解析代理）、`NewDirectRestyClient`（115 专用，永不读代理）、`NewFingerprintClient`（tls-client，代理构造时固定）。所有上游 HTTP 客户端只能从这里创建。
- **JavDB**：`javdb.Client.reinstall()` 订阅代理变更重建 transport，失败保留旧 transport 并 `slog.Warn`；自动路由时再触发 `Reselect`。`javbus.Probe / javdb.Probe` 只接收 `*url.URL` 与超时。
- **API**：`GET/PUT /api/settings/network`，`POST /api/settings/network/test`（并发探测 JavDB 与 JavBus，空请求体用当前配置）。校验错误 `domain.KindInvalid` 映射 400，前端直接展示后端消息。
- **前端**：设置页"网络代理"分区，一个开关、一个输入框、一个测试按钮，结果单条 toast 汇总。JavBus 数据开关留待工作线 C 加入同一分区。
- **遗留**：README 中 `MIYABI_PROXY` 说明由用户自行更新（AGENTS.md 禁止擅改 README）。

---

## 3. 工作线 B：结构重构

### 3.1 原则

- 行为不变：对外 HTTP 契约、数据库 schema、任务 payload 的 JSON 形状、NFO 输出全部保持。用黄金测试锁定。
- 叶子优先：先抽出被依赖最多、自身依赖最少的部分，再迁业务包。
- 每一步都是一个可独立合并的提交，`go test ./...` 全绿（`-race` 见第 8 节）。
- 迁移时顺手删冗余，但不做行为优化；行为优化列入 3.8 单独处理。
- 不引入类型别名或其它过渡脚手架，一次性原子替换。

### 3.2 步骤 B0：安全网（已完成）

- 端到端测试 `internal/service/pipeline_e2e_test.go`：内存假 115 + 固件 JavDB，跑"选目录 → 扫描 → 刮削 → 封面 → 上传"。未按原计划放 `internal/app`，B8 建包时随迁。
- 黄金测试 `internal/api/testdata/golden`：`/api/discover/movies/:id`、`/magnets`、`/api/library/movies`、`/api/tasks`、`/api/offline/tasks`、`/api/monitors`。
- `errorMiddleware` 映射测试、SSE 三类事件测试。
- 审计文档已删（`badcfa5`），`.gitattributes` 已加（`* text=auto eol=lf`）。

### 3.3 步骤 B1：`domain` 与错误（已完成，2026-09-21，提交 `7b077b3`、`ddcf80c`）

- `javdb/model.go` 的目录模型（`Movie、MovieDetail、Magnet、Actor、Tag、Zone、SearchOptions、BrowseOptions` 等）迁入 `internal/domain`，JSON tag 不变，全仓一次性替换；`service.DiscoverMovie` 等 DTO 内嵌 `domain.Movie`，黄金测试证明输出一致。`javdb.Options / RouteStatus / RouteCandidate / APIError / HTTPError` 留在 javdb。
- 错误模型：

```go
package domain
type Kind int // Invalid, Unauthorized, NotFound, Conflict, Busy, Upstream, Canceled, Internal
type Error struct { Kind Kind; Message string; Cause error }
func E(kind Kind, message string, cause error) *Error
func (e *Error) Error() string; Unwrap() error; PublicMessage() string
```

- `api/error.go` 只按 `Kind` 映射状态码，响应体只放 `Message`，`Cause` 进日志，sentinel switch 删除。内联中文 `fmt.Errorf` 全部改为 `domain.E(...)`。基础设施层错误类型通过 `DomainKind()` 与 `PublicMessage()` 接入。

### 3.4 步骤 B2：`tasks` 包（已完成，2026-09-21，提交 `2613122`、`fa9bab9`）

从 `service/task.go` 与 `worker/pool.go` 抽出：

| 文件 | 内容 |
| --- | --- |
| `kind.go` | `type Kind string`，`KindScan/KindScrape/KindCover/KindOffline` |
| `queue.go` | `Claim / Finish / Recover`，`Lock / Unlock` 队列门；不含领域规则，修订号由处理器 `Finished` 钩子以 `tasks.Change` 位掩码返回，事务提交后发布 |
| `registry.go` | `Register(Handler)`、`NewHandler(kind, handle, finished)`，`finished` 为 nil 时不实现 `FinishedHook` |
| `bus.go` | `Subscribe / Notify / Changed(Change) / Revisions`，一处扇出 |
| `service.go` | `EnqueueScan`（复用 Queued+Running）、`EnqueueFreshScan`（只复用 Queued）、`Workflows` |
| `workflow.go` | `List / Info`（scan+scrape+cover 折叠投影）、`EnsureScanTask` |
| `payload.go` | `EncodePayload / DecodePayload[T] / SetPayloadField`，`Path*` 常量与 `JSONExtract`；`database/indexes.go` 与业务层不再手写 JSON 路径 |
| `pool.go` | 单 worker pool，从注册表取处理器 |

- `movie.scrape_status` 的完成态修改移入 `scrape.Service.Finished`；失败返回 `ChangeLibrary|ChangeHistory`，成功返回 `ChangeOffline`；`library.Service.Finished` 返回 `ChangeOffline`。与原行为逐项等价。
- ⏭ `worker/offline.go`、`worker/monitor.go` 合并为 `tasks.RunPeriodic`，随 B5 / B6 处理。

### 3.5 步骤 B3：`drive` 包与 `Session`（已完成，2026-09-21，提交 `326f63e`、`9ba78b9`、`498bc3c`）

原 `service/pan*.go`（776 行）迁入 `internal/drive`：

| 文件 | 内容 |
| --- | --- |
| `drive.go` | `Drive`：凭据、挂载目录、授权版本、提交锁；`Open / OpenSource / Commit / Source / ValidateSource / StartWork / OpenMedia / SubscribeMount` |
| `account.go` | `Account / BeginLogin / LoginStatus / Disconnect`；账号校验带 60 秒 TTL 缓存 + singleflight |
| `mount.go` | `Files / SelectDirectory / ClearDirectory / DirectoryPath`；挂载目录只有内存快照 + 版本号一处真相 |
| `token.go` | `withPanToken` 主动/被动刷新，singleflight |
| `session.go` | `sourceSession` 实现 1.2 的 `Session`；`SourceInfo / DirectoryEntries` 助手 |
| `pagination.go` | `WalkFilePages / WalkOfflinePages` |
| `events.go` | `MountChanged`：`SelectDirectory` 先持久化并切换版本，再在提交锁内同步发布；任一监听者出错即回滚内存与 settings。`library` 订阅后调用 `tasks.EnqueueFreshScan`，`drive` 不 import `tasks` |
| `errors.go` | `ErrSourceChanged`（Conflict）、`ErrMediaDirectoryRequired`，全仓只此一份 |

- `PlayService`、`scrape.Service`、`OfflineService` 直接持有 `*drive.Drive`，`library.drive.*` 穿透归零；`DiscoverService` 通过构造参数注入 `SourceProvider` 窄接口（B7 `catalogue.LocalState` 前身）。
- `drive` 不导出测试钩子；原 `pan_*_test.go` 的 24 个并发用例迁入 `internal/drive`（`login_test / mount_test / token_test / pagination_test`），另补挂载回滚、会话失效矩阵、`ValidateSource`、离线分页用例。

### 3.6 步骤 B4 到 B8：业务包迁移

按顺序，每迁一个包提交一次。

**B4 `library` + `library/scan` + `library/scrape`（已完成，2026-09-21，提交 `a128ff6`、`b105941`）**

- `library_scan.go`（649 行）拆为 `scan/walker.go`（BFS、分页、`Scan` 调度骨架）、`scan/identity.go`（`identifyScanVideos`、`ResolveSingleNFO`）、`scan/persist.go`（`ProcessScanPageTx`）、`scan/reconcile.go`（`ReconcileScanTx`）、`scan/types.go`（`Payload`、`LibraryFiles`）。
- `scrape.go`（446 行）拆为 `scrape/scrape.go`（处理器）、`scrape/nfo_source.go`（`FindNFO / FindDirectoryNFO / ReadNFO / DirectoryNFO`，吸收了扫描侧重复的 NFO 读取）、`scrape/mapping.go`（`DetailNFO / MovieNFO`，双向映射一份）；`cover.go`、`metadata_snapshot.go → snapshot.go` 迁入。
- **番号容差校验**：`codeid.IsEquivalent(a, b)` 三级规则：规范化全等；数字核心一致且一方前缀是另一方前缀的后缀（`200GANA` ⊇ `GANA`，`CARIB` 与纯日期）；否则拦截。不维护任何静态字典。`scan/identity.go` 与 `scrape/nfo_source.go` 共用；独占单片目录下以 NFO 标准番号入库。
- **已浏览迁表**：`browse.viewed_movies` settings blob 迁为 `viewed_movie(javdb_id UNIQUE, viewed_at)`，`library.New` 时一次性迁移并删旧键，上限 5000 条按 `viewed_at` 淘汰。`DiscoverService` 不再持有用户状态，`api.ViewedManager` 由 `library.Service` 实现。
- `watch_history.go`、`library.go` 迁入 `library/`；`library_source.go` 与 `pan_directory.go` 的路径拼接合并为 `drive.DirectoryPath`；`service/library*.go、scrape.go、cover.go、metadata_snapshot.go、discover_viewed.go、scan_observations.go` 删除。

B4 与原计划的偏差与遗留（后续里程碑处理）：

1. `GET /api/discover/viewed` 仍整表下发（最多 5000 条），未做 `since` 增量或分页；前端 `browse-history-store.ts` 未改。留到 M8 与前端一起做。
2. `library.Service` 暴露 `Database / Drive / Tasks / Images` 四个 getter，仅 `service/play.go` 用 `Database()` 三处。B6 迁 `playback` 时 play 自持 ent 客户端，四个 getter 删除。
3. `library.New` 在构造函数内用 `context.Background()` 跑 `migrateViewedMovies` 且吞掉错误。B8 建组合根时移到启动迁移阶段，错误上抛。
4. `viewed_movie` 同批 upsert 共用一个 `now`，Windows 时钟粒度下相邻两次调用可撞同一时间戳，`TestViewedMovies_CRUDAndOrdering` 间歇失败（约 1/5）。修法：upsert 时取 `max(now, 表内最大 viewed_at + 1µs)`。**下一个提交先修。**
5. `scan.Scan(ctx, job, drive, db, images, tasks)` 六参数入口；`library.Service` 已持有全部依赖，B8 前改为 `scan.New(deps).Run(ctx, job)`。

**B5 `offline`（已完成，2026-09-22，提交 `3408cfc`）**：拆为 `add.go`（入口与锁）、`submit.go`（115 去重启发式）、`sync.go`（轮询与状态转换 `updateTask / markMissing / completeTask`）、`projection.go`（phase 计算）、`locks.go`。`offline.go:693` 对 `scan.Payload` 的直接构造改为调用 `library.EnqueueTargetedScan(...)`。`worker/offline.go` 并入 `tasks.RunPeriodic`。

**B6 `monitor`、`playback`、`maintenance`（已完成，2026-09-22）**：`playback` 拆为 `service.go / session.go / files.go / proxy.go / playlist.go`，自持 ent 客户端，不再经 `library.Database()`；`maintenance` 抽出自持数据目录；`monitor` 表兼容更名为 `subscription` 并扩展字段（见 4.5），自动迁移旧表数据；`library.Service` 四个依赖 getter 删除；`worker/monitor.go` 并入 `tasks.RunPeriodic`，`internal/worker` 目录彻底删除。

**B7 `catalogue`（已完成，2026-09-22）**：`discover.go、discover_cache.go、discover_tags.go、movie_state.go` 迁入 `internal/catalogue`。`MovieStates` 所需本地库状态通过 `catalogue.LocalState` 接口由 `library` 实现注入（`MatchingMovies`）。`DiscoverService.javdb` 改为 `catalogue.Provider` 接口，JavDB 是唯一实现；`Facets()` 暴露 zones、排序槽位供前端后续数据驱动（本轮前端不接）。


**B8 `app` 组合根**：`cmd/miyabi/main.go` 的 `run()` 拆为 `internal/app/app.go`，`New(cfg) (*App, error)`、`Run(ctx)`；任务处理器由各包 `Register`；`pipeline_e2e_test.go` 迁入；`internal/service` 目录删除（`access_gate.go`、`network.go`、`setting.go` 归 `app` 或 `api`）。

### 3.7 步骤 B9：API 层与配置

- `api/helpers.go`：`bindJSON / bindQuery / bindURI` 与 `respond(c, value, err)`、`accepted(c, value, err)`；保留流式与 SSE 特殊路径。约 60 处样板收敛。
- `Cache-Control: no-store` 现散在 `router.go`、`discover.go`、`offline.go` 五处，收敛为一个中间件。
- `config.Runtime`：收纳 pool 大小、离线轮询 30s、监控 5m、115 限速 2 req/s、超时 35s/45s/2m、播放会话 8h、缓存尺寸与 TTL、`minVideoSize`、视频后缀、sidecar 大小上限、JavBus 域名。环境变量 `MIYABI_*` 可覆盖，未设置用现值。
- 日志级别校验只留 `logging` 一处。

### 3.8 冗余与死代码清单

| 位置 | 处理 | 状态 |
| --- | --- | --- |
| `errPanSourceChanged` 与两处同文案字面量 | 统一为 `drive.ErrSourceChanged`；`ErrMediaDirectoryRequired` 同样只留 `drive` 一份 | ✅ B3 |
| `contextLock` 三份 | 收敛为 `syncx.ContextLock` | ✅ B3 |
| 扫描与刮削两份 NFO 读取 | 合并为 `scrape.ReadNFO / FindNFO / DirectoryNFO` | ✅ B4 |
| `service/*` 直接用 `slog.Default()` | 注入 logger | ✅ 已无 |
| `pan/*` 16 处 `json.Unmarshal + result.err()` 样板 | 抽 `apiRequest[T]`，仿现有 `authRequest[T]` | ⏳ |
| `pan/upload.go` ≤128KiB 时两次 SHA1 | 复用 | ⏳ |
| `javdb/transport.go:82` 先读全 body 再判状态码 | 先判状态码，非 2xx 只做有上限 drain | ⏳ |
| `javdb` 3 处 `slog.Warn` 与 `WarnContext` 混用 | 统一 `WarnContext` | ⏳ |
| `javdb/client.go:41` 字段 `selectRoute` 与包级函数同名 | 字段改名 `selector` | ⏳ |
| `pan/play.go` 基础设施层中文文案 | 改为 `domain.E` 由上层赋文案 | ⏳ |
| `pan/file.go` `Count/Size` 未用 `json.Number` | 与同结构其它字段一致 | ⏳ |
| `worker/offline.go`、`worker/monitor.go` 两个相同的 ticker 循环 | 合并为 `tasks.RunPeriodic(name, interval, wake, fn)` | ✅ B5/B6 |
| `library.Service` 四个依赖 getter | 随 B6 删除 | ✅ B6 |
| `internal/ent/enttest` 生成但未使用 | 保留（生成物） | — |
| 前端 `api/library.ts`、`api/offline.ts`、`api/watch-history.ts` 反向 import features | toast 移到调用方；`watchSessions` 移入 `features/player` | ⏳ 3.9 |

### 3.9 前端优化与拆分清单

原则：不改任何 className、动效、骨架屏数量与路由结构；每项以"渲染 DOM 一致"为验收。已完成：`ListPagination` 收敛到 shadcn `ui/pagination.tsx`，页码算法抽为 `lib/pagination.ts` 纯函数并配单测（`58622d6`、`1b64d52`、`09e1b6f`，清单外顺手项，见第 9 节）。按优先级：

**结构**

1. `src/api` 变叶子层：`api/library.ts`、`api/offline.ts` 的 toast 移到调用组件的 `onSuccess/onError`；`api/watch-history.ts` 依赖的 `watchSessions` 移入 `features/player`。
2. `features/player/movie-player.tsx`（319 行）拆为 `movie-player.tsx`（数据加载与文件选择）、`playback-player.tsx`（Vidstack 装配）、`use-playback-source.ts`（画质切换、重试、续播 seek）。
3. `features/discover/page.tsx`（246 行）把 `CategoryContent / BrowseResults / CategoryFilters` 拆成独立文件；`CategoryFilters` 的 13 个 props 全部来自 store，改为组件内直接读 store。
4. `features/history/page.tsx`（247 行）拆出 `use-history-selection.ts` 与 `history-clear-dialog.tsx`。
5. `features/tasks/task-notifications.tsx` 的 97 行 `useEffect` 抽成纯函数 `diffTaskNotifications(prev, next)` 并补单测；`version: JSON.stringify([...])` 改为显式比较。
6. `features/settings/pan-directory-dialog.tsx`（215 行）拆 `directory-breadcrumbs.tsx` 与 `directory-list.tsx`。
7. `api/movie-detail-cache.ts` 的 `findCachedMovieCard` 全量扫描改为维护 `id → 最新卡片` 索引。
8. `browse-history-store.ts` 配合后端 `GET /api/discover/viewed` 改为 `since` 增量拉取（B4 遗留 1）。

**去重**

9. 三份 `page` 校验器 → `lib/search-schema.ts`。
10. 三处 `DiscoverResults` 参数展开 → 组件直接接收 `UseQueryResult`。
11. 两处 zone Select、两处确认对话框、两处卡片包装、四处 `sameSource` 判断、五处 `clamp` 各收敛为一份。
12. 三种 entity 形状与两种 source scope 统一为一份类型与转换函数。
13. `MagnetCard` 9 个 props 中 5 个派生自两个查询，卡片自己订阅；配合工作线 C 改为标签驱动。
14. 查询默认项（`retry:false, refetchOnWindowFocus:false`）收敛到 `QueryClient` 默认或共享工厂。
15. `discoverKeys` 移回 `discover.ts`；`watchHistoryKeys` 嵌套在 `libraryKeys` 下的隐式耦合加注释或拆开。

**卫生**

16. `lib/watch-progress.ts` 与 `features/player/watch-progress.ts` 撞名，后者改 `watch-progress-writer.ts`。
17. `task-progress-state.ts` 与 `scan-status.ts` 两套阶段映射共享阶段定义。
18. `shadcn` 移到 `devDependencies`；`ui/card.tsx`、`ui/tabs.tsx` 零引用导出按需清理（`ui/pagination.tsx` 已被 `ListPagination` 全量引用，不清理；`GoogleCastButton` 是 Vidstack 类型必填项，不能删）。
19. `client.ts` 的 `imageURL` 特判 `/api/library/artwork/` 前缀，改为后端统一返回可直接使用的 URL 后删除。

不在本轮：发现页状态迁 URL、facets 数据驱动、类型生成。

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

| 序 | 里程碑 | 内容 | 状态 | 依赖 |
| --- | --- | --- | --- | --- |
| M0 | 安全网 | B0 | ✅ | 无 |
| M1 | 全局代理 | 工作线 A | ✅ 2026-09-20 | 无 |
| M2 | 模型与错误 | B1 | ✅ 2026-09-21 | M0 |
| M3 | 任务与会话 | B2、B3 | ✅ 2026-09-21 | M2 |
| M4 | 业务包迁移 | B4 ✅ 2026-09-21；B5、B6、B7 ✅ 2026-09-22；B8 待做 | 进行中 | M3 |
| M5 | API 与配置收口 | B9、3.8 后端清单 | 待做，约 4 天 | M4 |
| M6 | 磁力聚合 | 工作线 C | 待做，1 到 1.5 周 | M1、M2 |
| M7 | JavDB 接口补齐 | 工作线 D | 待做，3 到 5 天 | M2 |
| M8 | 前端结构清理 | 3.9 清单、`/api/discover/viewed` 增量 | 分页已收敛，其余待做，约 1 周 | 可与 M4 到 M5 并行 |
| M9 | 字幕自动化与播放集成 | 工作线 E | 待做，4 到 5 天 | M2、M5 |

剩余约 6 到 7 周单人工作量。串行顺序 M4（B5 → B8）→ M5 → M6 → M7 → M9；M8 并行。下一步：B8（`app` 组合根落地，彻底移除 `internal/service`）。


---

## 8. 验证与回归边界

- 每个提交：`go test ./...`、`go vet`、前端 `npm test`、`tsc --noEmit`、oxlint、Vite 构建。间歇失败视为红。
- `-race` 需要 cgo；Windows 开发机默认 `CGO_ENABLED=0` 跑不了，在 Linux（Docker 构建镜像或 WSL）上跑 `go test ./... -race`，至少每个里程碑收口时跑一次。
- M2 起：黄金 JSON 测试证明所列端点响应逐字节一致。
- M3：`drive` 并发测试覆盖登出、换目录、令牌刷新中途发生三类场景（已迁入 `internal/drive`）。
- M4：e2e 测试在每个包迁出后重跑；`internal/service` 删除时 e2e 必须仍然通过。
- M6：JavBus 固件测试；聚合器测试覆盖单源超时、单源失败、重复 infohash 合并、`Inferred` 标记、排序稳定性。
- 浏览器行为按项目约定由你验证：设置页网络分区、磁力卡片徽章、排行标签页、评论折叠区、分页器。

---

## 9. 决定记录

**架构**

- 包名沿用 `database`，不改 `storage`；新增 `syncx` 放 `ContextLock`。
- 模型迁移（2026-09-20）：不用类型别名过渡，全仓一次性原子替换。
- 错误模型（2026-09-21）：`domain.Error` 不实现自定义 `Is`，哨兵按指针身份匹配，分类走 `domain.IsKind / KindOf`。基础设施层错误类型（`pan.apiError`、`javdb.APIError / HTTPError / networkError`）通过 `DomainKind()` 与 `PublicMessage()` 接入，不在业务层逐个翻译。
- 任务引擎（2026-09-21）：`tasks.Queue` 不含领域规则；修订号由处理器 `Finished` 钩子以 `tasks.Change` 位掩码返回，队列在事务提交后统一发布；无钩子的处理器不触发修订。
- 挂载与扫描（2026-09-21）：挂载不再与扫描入队同事务。drive 先持久化并切换版本，再在提交锁内同步发布 `MountChanged`；任一监听者出错即回滚内存与 settings，对外等价于原子。监听者不得经由 drive 开会话或提交（死锁），约束写在 `SubscribeMount` 注释。挂载触发的扫描只复用 `Queued`（`EnqueueFreshScan`），手动重扫复用 `Queued+Running`（`EnqueueScan`）。
- `drive.Session`（2026-09-21）：承担全部 115 访问，含播放地址与离线任务；`CommitAccount` 只复查凭据不复查目录，离线任务换目录后仍可落账。`drive` 不导出只为测试存在的方法，测试走真实 QR 登录与 `SelectDirectory`。
- 番号识别（2026-09-20 决定，`498bc3c` 落地）：不引入静态前缀字典；`codeid.IsEquivalent` 核心数字一致且前缀包含时放行并收敛为 NFO 标准番号。
- 已浏览（2026-09-21）：只存 JavDB ID 不存番号；`browse.viewed_movies` 迁 `viewed_movie` 表，启动时一次性迁移并删旧键；API 路径与 JSON 形状不变，增量拉取留 M8。
- `cover` 任务 payload 的 `artworkOrigin` json 标签保持 B1 之前形状（`9ba78b9` 还原了 `326f63e` 的误删）。

**网络与代理**

- 一个开关一个地址；开启时 JavDB 与 JavBus 走代理，115 永远直连。JavBus 无镜像，不做端点管理。
- `MIYABI_PROXY` 彻底删除，不作初始种子；代理密码不脱敏；校验错误中文化并映射 400（`domain.KindInvalid`）。
- `Normalize` 在地址为空时把 `Enabled` 归一为 false 并持久化，"开启但无地址"不是合法状态。
- `golang.org/x/net/html` 提升为直接依赖（工作线 C）。

**产品**

- JavBus 数据默认关闭，设置页可开；磁力按来源加徽章。
- 追踪改为订阅，支持影片与演员；自动推送默认值在设置页选择；独立路由 `/subscriptions` 进 `FloatingNav`；单部、多选、一键入库，批量走任务队列。
- `config.Runtime` 的环境变量先内部集中，对外开放的在实现时逐个写进 README。
- 审计文档已删除（`badcfa5`）。

**前端样式例外**（"原样沿用"约束的两次例外，3.9 清单以此为新基线）

- 徽章（2026-09-21，`23c58cc`）：Badge 新增 `library`（紫色，已入库/新入库）与 `frosted`（磨砂，番号/下载中）两个变体，"预览"文案改为"有预览"。
- 分页器（2026-09-21，`562f49d`、`58622d6`、`1b64d52`、`09e1b6f`）：`ListPagination` 改为 shadcn `PaginationLink / PaginationEllipsis` 页码链接，库页面移除"共 N 部影片 · 每页 20 部"文案，单页时隐藏。页码算法在 `lib/pagination.ts`：连续窗口 3 页（当前页 ±1），首尾页始终可点，总页数 ≤7 时全列，省略号不用于只遮一页。当前页 `aria-current="page"` 不可点，禁用态 `aria-disabled` + `pointer-events-none`；上一页/下一页保持原生 `Button`。传 `totalPages` 的页面（库、观看历史）渲染完整页码，发现页只渲染当前页占位。`562f49d` 的页码输入框已被页码链接替代。
