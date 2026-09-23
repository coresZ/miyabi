# Miyabi 重构方案

- 日期：2026-09-19；最近更新 2026-09-23
- 基线：`master / 85228f7`；当前进度基线 `61fbcba` 加番号容差增强
- 范围：五条工作线。① 全局代理；② 整体结构重构（含冗余清理）；③ 磁力聚合（JavDB + JavBus）；④ javdb-cli 接口补齐评估；⑤ 字幕自动化与播放集成。
- 约束：前端所有样式与动效原样沿用，前端只做结构性调整（已记录三次例外，见第 9 节）；预览视频、DMM、第三方图库不在本轮范围。
- 进度：M0 到 M6 已完成并收口（M6 含订阅）；M8 前端结构清理已完成（8da0c34）；工作线 D 番号容差增强（ResolveMovieID 格式等价与补零容差）已落地；优先开始工作线 E（字幕自动化与播放集成）。

---

## 目录

1. 目标架构
2. 工作线 A：全局代理
3. 工作线 B：结构重构
4. 工作线 C：磁力聚合与订阅
5. 工作线 D：javdb-cli 接口评估
6. 工作线 E：字幕自动化与播放集成
7. 执行顺序与里程碑
8. 验证与回归边界
9. 决定记录

---

## 1. 目标架构

### 1.1 包布局（✅ 已就位，⏳ 待做）

```
cmd/miyabi/                 ✅ main.go 只解析参数、日志与信号，装配在 internal/app（B8）
internal/
  app/                      ✅ 组合根 New / Run / Close / CheckHealth 与 NetworkService（B8）
  config/                   ✅ 四个 MIYABI_* 环境变量 + config.Runtime 五个内部常量（B9）
  domain/                   ✅ 纯模型与错误（B1）
  netx/                     ✅ 代理管理器、HTTP 客户端工厂、探测类型（A、B8）
  database/                 ✅ ent 客户端、迁移、原生索引、monitors → subscriptions 数据迁移、`LoadSetting / SaveSetting`（方案原名 storage，沿用现名）
  syncx/                    ✅ ContextLock（B3）
  tasks/                    ✅ 队列、pool、注册表、SSE 总线、typed payload、RunPeriodic、订阅批量任务投影（B2、B5、C）
  drive/                    ✅ 115 账号、挂载目录、Session（B3）
  pan/ javdb/               ✅ 现有客户端
  javbus/                   ✅ 探测、详情页与磁力片段客户端（C）；端点内置，无镜像
  library/                  ✅ 索引、观看记录、已浏览、EnqueueTargetedScan、LocalState 实现（B4）
  library/scan/             ✅ Scanner：walker / identity / persist / reconcile（B4）
  library/scrape/           ✅ scrape / nfo_source / mapping / cover / snapshot（B4）
  catalogue/                ✅ Provider / LocalState 接口、缓存、标签、本地状态投影、Facets（B7）
  offline/                  ✅ add / submit / sync / projection / locks（B5）
  monitor/                  ✅ 影片与演员订阅、每日检查、单部与批量入库（B6、C）
  playback/                 ✅ session / files / proxy / playlist（B6）
  maintenance/              ✅ 数据目录统计与缓存清理（B6）
  magnet/                   ✅ Source 接口、Aggregator、quality 推断、Picker（C）
  api/                      ✅ gin 路由、错误映射、DTO、访问密码门、bind/respond 助手、noStore 中间件、订阅与 JavBus 端点（B9、C）
  image/ nfo/ codeid/ logging/   不动（codeid 新增 IsEquivalent）
```

`internal/service` 与 `internal/worker` 已删除。

依赖方向自上而下单向：`api → 业务包 → drive/tasks/catalogue → pan/javdb/javbus/netx → domain`。业务包之间不得互相 import 具体类型，只能通过在 `domain` 或调用方定义的接口交互。`library/scan → library/scrape` 是同一限界上下文内的子包共享，允许；反向不得出现。

依赖方向由 `internal/app/deps_test.go` 锁定：解析每个业务包与 `drive / tasks / database` 的非测试文件 import，业务包之间、内核包对业务包出现 import 即失败。跨包共享的 DTO（`LibraryFile / WatchHistoryScope / WatchResume / MovieSummary / Media / OfflineSubmission`）放 `domain/library.go`；跨包共享的查询范围（`LibraryFiles / WatchHistory` 谓词）放 `database/scope.go`；settings 表的 JSON 读写只有 `database.LoadSetting / SaveSetting` 一份。

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
- `domain.Magnet` 新增 `Sources []string`、`Tags []string`、`Inferred bool`；标签词表与来源名是 `domain.MagnetTag* / MagnetSource*` 常量。`HasSubtitle / HD` 只表示站方标注，推断只写 `Tags`，因此 `Tags` 含字幕而 `HasSubtitle` 为假即可判定为推断。现有字段和 JSON 名保持不变。

---

## 2. 工作线 A：全局代理（已完成，2026-09-20）

- **配置**：settings 表 key `network.proxy`，`{ "enabled": bool, "url": string }`。一个开关一个地址；开启时 JavDB 与 JavBus 走代理，115 永远直连。支持 `http:// https:// socks5://` 带用户名密码，不脱敏。`MIYABI_PROXY` 与 `config.Proxy` 已删除，settings 无记录时直连。
- **`internal/netx`**：`ProxyManager`（`Config / Resolve / Update / Subscribe`）与三个客户端工厂 `NewRestyClient`（按请求解析代理）、`NewDirectRestyClient`（115 专用，永不读代理）、`NewFingerprintClient`（tls-client，代理构造时固定）。所有上游 HTTP 客户端只能从这里创建。
- **JavDB**：`javdb.Client.reinstall()` 订阅代理变更重建 transport，失败保留旧 transport 并 `slog.Warn`；自动路由时再触发 `Reselect`。`javbus.Probe / javdb.Probe` 只接收 `*url.URL` 与超时。
- **API**：`GET/PUT /api/settings/network`，`POST /api/settings/network/test`（并发探测 JavDB 与 JavBus，空请求体用当前配置）。校验错误 `domain.KindInvalid` 映射 400，前端直接展示后端消息。
- **前端**：设置页"网络代理"分区，一个开关、一个输入框、一个测试按钮，结果单条 toast 汇总。代理只管网络；JavBus 数据源开关是独立的"JavBus"分区（工作线 C）。
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

- 端到端测试 `internal/app/pipeline_e2e_test.go`（B8 前在 `internal/service`）：内存假 115 + 固件 JavDB，跑"选目录 → 扫描 → 刮削 → 封面 → 上传"。
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

### 3.6 步骤 B4 到 B8：业务包迁移（已完成，2026-09-22）

每迁一个包一个提交：B4 `a128ff6`、`b105941`；B5 `3408cfc`；B6 `ccf1750`；B7 `118c8a7`；B8 `9b4d21f`。

| 步骤 | 落地内容 |
| --- | --- |
| B4 `library` | `library_scan.go` 拆为 `scan/walker.go`（`Scanner` 调度骨架）、`identity.go`（`ResolveSingleNFO`）、`persist.go`、`reconcile.go`、`types.go`；`scrape.go` 拆为 `scrape/scrape.go`、`nfo_source.go`（`FindNFO / ReadNFO / DirectoryNFO`，吸收扫描侧重复读取）、`mapping.go`（双向映射一份）、`cover.go`、`snapshot.go`。`codeid.IsEquivalent` 三级容差（规范化全等；数字核心一致且前缀互为后缀；否则拦截），不维护字典，扫描与刮削共用，独占单片目录以 NFO 标准番号入库。`browse.viewed_movies` blob 迁 `viewed_movie` 表，启动时 `library.MigrateViewedMovies` 一次性迁移；同批 upsert 的 `viewed_at` 取 `max(now, 表内最大值 + 1µs)` 防撞。`drive.DirectoryPath` 合并两处路径拼接 |
| B5 `offline` | `add.go`（入口与锁）、`submit.go`（115 去重启发式）、`sync.go`（轮询与状态转换）、`projection.go`（phase 计算）、`locks.go`。只依赖自定义接口 `Catalogue`（`HasMagnet / MovieCode`）与 `TargetedScanner`（`library.Service.EnqueueTargetedScan`，事务内建任务）。`worker/offline.go` 并入 `tasks.RunPeriodic(ctx, logger, name, interval, wake, fn)` |
| B6 `playback` `monitor` `maintenance` | `playback` 拆为 `service / session / files / proxy / playlist`，自持 ent 客户端。`monitor` 表兼容更名 `subscriptions`，字段按 4.5 扩展（`kind / target_id / origin_id / auto_download / zone / cursor`，`status` 枚举扩到六值）；`database.migrateSubscriptions` 在 ent 建表后 `INSERT OR IGNORE … SELECT … FROM monitors; DROP TABLE monitors`，可重入；`/api/monitors*` 路径与响应不变，换路径留给工作线 C。`maintenance` 自持数据目录。`worker/monitor.go` 并入 `RunPeriodic`，`internal/worker` 删除。`library.Service` 四个依赖 getter 删除 |
| B7 `catalogue` | `discover*.go、movie_state.go` 迁入。`Provider` 接口（JavDB 唯一实现）、`LocalState` 接口（`Source / MatchingMovies`，由 `library.Service` 实现，`domain.LocalMovie` 新增），`Facets()` 暴露 zones 与排序槽位（前端本轮不接） |
| B8 `app` | `internal/app/app.go`：`New(cfg, logger) / Run(ctx) / Close() / CheckHealth(listen)`，处理器注册与 `RunPeriodic` 启动集中在此；`main.go` 收到 52 行。`access_gate` 归 `api`，`network / setting` 归 `app`，探测类型进 `netx`。e2e、drive 夹具、task 测试迁入 `app`。`internal/service` 删除 |

**M4 收口（2026-09-22）**：以下八条已清，不再有业务包之间的具体类型依赖，仓库内不再有 `type X = Y` 别名。

1. `catalogue.MovieSummary` 改返回 `domain.MovieSummary`，`catalogue` 不再 import `monitor`。
2. `offline.Submission` 迁为 `domain.OfflineSubmission`（`Status` 由 ent 枚举改为 `string`，JSON 不变），`monitor.OfflineAdder` 与 `api.OfflineManager` 都引用 `domain`。
3. `library.File / WatchHistoryScope / WatchResume` 迁 `domain`；`scan.LibraryFiles` 与两份重复的 `historyScope` 合并为 `database.LibraryFiles / WatchHistory`；`playback` 只 import `database / drive`。
4. `maintenance.New` 改收 `ArtworkLocker` 接口（`TryLockArtwork / UnlockArtwork`）。
5. `catalogue`、`library`、`offline`、`maintenance`、`playback` 共 17 个兼容别名删除。
6. `scan.Scan` 并入 `Scanner.Run`；`library.EnqueueTargetedScan` 只留方法。
7. `app.Run` 统一退出路径：任一 goroutine 出错或 ctx 取消，都 `cancel → Shutdown(10s) → 等 pool 与两个周期任务退出`，`Shutdown` 错误随 `errors.Join` 返回，与原 `main.go` 等价。
8. `javdb.Media` 迁 `domain.Media`；`api.Dependencies` 字段与接口改名 `Catalogue / CatalogueManager`、`Drive / DriveManager`、`Maintenance / MaintenanceManager`。`catalogue.Provider` 仍暴露 `javdb.RouteStatus`：路由是 JavDB 客户端的运维概念（B1 决定），不迁 `domain`。

仍待做：`GET /api/discover/viewed` 整表下发（最多 5000 条），`since` 增量留 M8 与前端一起做。

### 3.7 步骤 B9：API 层与配置（已完成，2026-09-22，提交 `fe6cc25` 及收口）

- `api/helpers.go`：`bindJSON / bindQuery / bindURI[T]`、`respond / accepted / created`，78 处样板收敛，配 `helpers_test.go`。流式与 SSE 路径保留（SSE 自带 `no-cache, no-transform`，不走 `noStore`）。
- `noStore()` 中间件一份，路由组与单端点按需挂载，散落的 9 处 `c.Header` 删除。
- `config.Runtime` 只做内部常量集中，**不新增环境变量**（对外仍只有 `MIYABI_LISTEN / DATA_DIR / LOG_LEVEL / ACCESS_PASSWORD` 四个）。字段只在 `app.New` 有注入点时才存在，现为五个：`TaskPoolWorkers`、`OfflineSyncInterval`、`MonitorCheckInterval`、`OfflineSubmitTimeout`、`PlaybackSessionTTL`。上游客户端与缓存的常量留在各自包内命名（`pan.requestTimeout / requestGap`、`drive.upstreamTimeout`、`catalogue.*CacheSize / *CacheTTL`），不经 Runtime 中转；`fe6cc25` 引入的 13 个 `MIYABI_*` 读取与 16 个无消费者字段随收口删除。
- 日志级别校验收敛为 `logging.Validate`。
- 收口时修复 `fe6cc25` 的一处回归：`pan/play.go` 文案迁到 `drive.ErrTranscodeUnavailable` 后，`playback` 用 drive 哨兵去匹配 pan 返回的裸哨兵，永远不中，115 未转码从 502 退化为 500。改为匹配 `pan.ErrTranscodeUnavailable` 再翻译，`playback/transcode_test.go` 从 pan 返回值一路断言到 `KindUpstream` 与用户文案。

### 3.8 冗余与死代码清单

| 位置 | 处理 | 状态 |
| --- | --- | --- |
| `errPanSourceChanged` 与两处同文案字面量 | 统一为 `drive.ErrSourceChanged`；`ErrMediaDirectoryRequired` 同样只留 `drive` 一份 | ✅ B3 |
| `contextLock` 三份 | 收敛为 `syncx.ContextLock` | ✅ B3 |
| 扫描与刮削两份 NFO 读取 | 合并为 `scrape.ReadNFO / FindNFO / DirectoryNFO` | ✅ B4 |
| `service/*` 直接用 `slog.Default()` | 注入 logger | ✅ 已无 |
| `pan/*` 16 处 `json.Unmarshal + result.err()` 样板 | 抽 `apiRequest[T]`，仿现有 `authRequest[T]` | ✅ M5 |
| `pan/upload.go` ≤128KiB 时两次 SHA1 | 复用 | ✅ M5 |
| `javdb/transport.go:82` 先读全 body 再判状态码 | 先判状态码，非 2xx 只做有上限 drain | ✅ M5 |
| `javdb` 3 处 `slog.Warn` 与 `WarnContext` 混用 | 统一 `WarnContext` | ✅ M5 |
| `javdb/client.go:41` 字段 `selectRoute` 与包级函数同名 | 字段改名 `selector` | ✅ M5 |
| `pan/play.go` 基础设施层中文文案 | 改为 `domain.E` 由上层赋文案 | ✅ M5（收口补了映射与测试） |
| `pan/file.go` `Count/Size` 未用 `json.Number` | 与同结构其它字段一致 | ✅ M5 |
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

## 4. 工作线 C：磁力聚合与订阅（已完成，2026-09-22，提交 `217e28b`、`e0de46b` 及收口）

### 4.1 为什么只要 JavDB + JavBus

两者都是人工维护的聚合站，磁力经过筛选并带站方标注的"高清 / 字幕"标签，质量远好于 Sukebei 这类原始索引；收录互补（JavBus 常先收录 DMM 系新片，旧片保留更久），合并去重后覆盖面明显提升。两者都以番号为键，不需要身份映射。

### 4.2 `internal/javbus`

- **端点**：只有 `https://www.javbus.com` 一个，内置为包常量；没有镜像，不提供自定义地址。
- **传输**：`netx.NewFingerprintClient`（Chrome 指纹，前面有 Cloudflare）、cookie jar、限速 1 req/s、超时 15s；订阅代理变更重建客户端，失败保留旧客户端并 `WarnContext`。
- **协议**（2026-09-19 通过本机代理实测，固件在 `testdata/`）：详情页 `GET /{CODE}?existmag=all` 必须带 `Cookie: dv=1`，否则 302 到 `/doc/driver-verify`；页内脚本给出 `gid / uc / img`；磁力片段 `GET /ajax/uncledatoolsbyajax.php?gid=&lang=zh&img=&uc=&floor=`，带 `Referer` 与同一 cookie，返回若干 `<tr>`：第 1 列 `a[href^=magnet:]`（hash 小写归一）与 `btn-primary`（高清）、`btn-warning`（字幕），第 2 列体积（1024 进制），第 3 列日期（存入 `CreatedAt` 供排序，界面不展示）。番号不存在返回真实 404，视为无结果。`Just a moment` 与 `driver-verify` 识别为 `KindUpstream`。
- **跳过**：`ZoneWestern / ZoneAnime / ZoneFC2` 与 FC2 前缀番号静默跳过（JavBus 无这些专区，FC2 收录零散），由 JavDB 单独覆盖。
- **缓存**：详情页参数按番号缓存 5 分钟，上限 512 条。
- **解析**：`golang.org/x/net/html`，已提升为直接依赖。

### 4.3 `internal/magnet`

- `Source` 接口（1.2）；`javdb.Client` 与 `javbus.Client` 各实现一份，来源名为 `domain.MagnetSourceJavDB / JavBus`。
- `Aggregator.Find`：每源独立 `context.WithTimeout`（8s）并发，单源失败或超时只记 `WarnContext`，全部失败才返回 `KindUpstream`。同 infohash 合并：`Sources` 追加并保持 javdb 在前、`HasSubtitle / HD` 取或、`Size / FilesCount` 取最大、名称取 JavDB、`CreatedAt` 取最早、`Tags` 并集。排序：字幕 > 高清 > 体积 > 文件数，同分 javdb 在前，再按最新。
- `quality.Infer / ApplyInference`：移植 JHS 的 `classifyQuality`（先剥广告括号；`FHDC`、`-C / -UC`、中文关键词、含汉字无假名 → 字幕；`4K / 2160P` → 4K；`uncensored / mosaic / -U / 无码` → 无码；`破解 / 流出 / leaked` → 破解）。推断只追加 `Tags`，绝不改写站方的 `HasSubtitle / HD`；有新增标签才置 `Inferred`，重复应用是空操作。`HasSubtitle / IsHD / IsUncensored` 三个谓词是聚合器、Picker 与调用方的唯一判定入口。
- `Picker`：偏好三项（字幕、高清各"优先 / 必须 / 不限"，无码"优先 / 必须 / 排除 / 不限"），先按"必须 / 排除"过滤，再按"优先"计分（字幕 10000 > 高清 1000 > 无码 100，推断属性减半：站方未标而名称暗示的字幕、由 4K 推出的高清、永远是推断的无码），同分沿用聚合器的次序。`Preferences.Normalized / Validate` 供设置写入校验。
- `catalogue.Magnets` 的 64 条 / 1 分钟缓存改缓存聚合结果；JavBus 开关切换时清空。

### 4.4 接入点与设置

- `GET /api/discover/movies/:id/magnets`：每项含 `sources / tags / inferred`（黄金文件已更新）。`POST …/offline {hash}` 对聚合结果校验，JavBus 独有磁力也能推送。
- `catalogue` 持有 JavBus 开关：`javbus.enabled`（默认关），`GET/PUT /api/settings/javbus`，运行时以原子布尔门控聚合源（`gatedSource`），不重建客户端。关闭时聚合器只跑 JavDB，行为与 M5 之前一致。
- 前端：设置页新增独立"JavBus"分区（一个开关，切换后失效影片详情缓存）；"网络代理"分区回到只管代理。`MagnetCard` 遍历 `sources`（JavDB / JavBus 徽章）与 `tags`（字幕 / 高清 / 4K / 无码 / 破解）渲染，沿用 `variant="outline"`，两个布尔徽章分支删除。
- 不做的：`monitor.sources` 来源策略（Picker 同分时已优先 javdb，再加开关无收益）、`magnet.javbus.base_url / mirrors`（单端点）、体积上限（用户决定不需要）。

### 4.5 订阅（影片 + 演员）

**数据模型**：`subscriptions` 表（M4 迁自 `monitors`）：`kind`（movie / actor）、`target_id`（`(kind, target_id)` 唯一）、`code / title / cover / release_date`（演员复用 `title / cover` 存名字与头像）、`origin_id`（由演员订阅派生的影片订阅指向来源）、`auto_download`、`zone`、`status`（影片 `waiting / added / stale`，演员 `active / paused / error`）、`cursor`（演员：已见影片 id 集合与已发售水位线）、`hash / task_id / next_check_at / last_checked_at / checks / error`。

**设置**（`subscription.config`，设置页"订阅设置"分区，改动即保存）：影片默认自动入库（默认开）、演员新作默认自动入库（默认关）、每日检查时间（`HH:MM`，默认 04:00，影片与演员共用）、磁力偏好三项（默认字幕优先、高清优先、无码不限）。写入经 `Config.validate`，非法值 400。

**检查**（`tasks.RunPeriodic`，`MonitorCheckInterval` 5 分钟一轮，只处理到期项）：
- 影片：未发售每三天、发售当天起每天一次，都落在每日检查时间；发售 30 天后仍无磁力转 `stale`。取聚合磁力经 Picker 选一条；`auto_download` 开则推 115 转 `added`，关则记下 `hash` 等用户入库并次日再查。上游失败一小时后重试，错误文案写入 `error`。
- 演员：每天一次取 `Browse(actor, release desc, 40)`。订阅时以该页做游标基线，只对未发售作品立即建影片订阅（继承 `auto_download / zone`，`origin_id` 指向演员）；之后每次把"未见过且发行日期不早于水位线减 30 天"的作品当新作派生订阅，避免游标丢失时把旧作全部推入 115。派生添加绝不重置已有订阅；用户手动重复添加已入库 / 过期的影片才重置。`Browse` 失败时不创建演员订阅。已见列表保留最新 300 个 id。

**入库**：`POST /api/subscriptions/:id/enqueue` 单部：Picker 选中即推 115 转 `added`；无合格磁力则保持 `waiting` 并置 `auto_download=true`；已 `added` 直接返回不重复推送；演员订阅返回 400。`POST /api/subscriptions/enqueue {ids | all}` 创建 `subscription_batch` 任务由单 worker 顺序处理，每部间隔 1.5 到 3 秒随机；payload 记录 `total / processed / submitted / waiting / failed / failures[]`（最多 20 条明细），`tasks.ListWorkflows` 把最近 5 条与进行中的批量任务并入 `/api/tasks`，前端 `TaskNotifications` 以 toast 展示进度与失败明细。空选择返回 400。

**API**：`GET /api/subscriptions?kind=&page=&limit=`、`POST /api/subscriptions {kind, target_id, title?, cover?, auto_download?, zone?}`、`PATCH /api/subscriptions/:id {auto_download?, zone?, status?}`（`status` 只接受演员的 `active / paused`）、`DELETE /api/subscriptions/:id`、上面两个 enqueue、`GET /api/subscriptions/actors/:id/feed`、`GET/PUT /api/settings/subscription`。`/api/monitors*` 已删除。

**前端**：`FloatingNav` 新增"订阅"（`Bell`），路由 `/subscriptions`，`Tabs` 两页。影片订阅页复用 `MovieGridLayout / MovieCard`，状态徽章沿用 `Badge` 变体，`MovieStateBadge` 让已入库影片不显示"入库"按钮；选择模式与"全选待入库 / 入库选中 / 一键入库"照搬历史页（卡片包 `Button` 加右上角 `Checkbox`，选中 `ring-2 ring-success`，确认用现有 `Dialog`）。演员订阅页上方是可横向滚动的演员条目（`Avatar` + 名称 + 新作数 + 自动入库 `Switch` + 暂停 / 取消），点选后网格只显示其新作，未选中显示全部派生新作，网格与影片页共用同一组件。演员作品页头部有"订阅演员"，详情页与即将发行卡片的追踪按钮改为"订阅"。

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
| `ResolveMovieID` 的"格式等价唯一匹配" | 番号解析 | **已落地** | 分隔符差异（`ABC00123` vs `ABC-123`）与补零容差解析，多候选拒绝；2026-09-23 完成 |
| 自动选线 `SelectAutoHost` | 路由 | 不动 | miyabi 现有实现更完整（持久化、手动选择、故障切换） |
| 以图搜番（avscan.cc） | 反搜 | 后议 | 上传截图到第三方，隐私边界需要你决定 |
| 资源下载与 HLS→MP4 重封装 | 预览视频 | 后议 | 属于预览视频范围 |

工作线 D 的番号容差规则已落地；排行、评论及实体详情暂缓，优先推进工作线 E。

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
| M4 | 业务包迁移 | B4 到 B8 | ✅ 2026-09-22 | M3 |
| M5 | API 与配置收口 | B9、3.8 后端清单 | ✅ 2026-09-22 | M4 |
| M6 | 磁力聚合与订阅 | 工作线 C | ✅ 2026-09-22（`217e28b`、`e0de46b` 及收口） | M1、M2 |
| M8 | 前端结构清理 | 3.9 清单、`/api/discover/viewed` 增量 | ✅ 2026-09-22（`8da0c34` 收口） | 可与 M5 并行 |
| M9 | 字幕自动化与播放集成 | 工作线 E | ✅ 2026-09-23 | M2、M5 |
| M7 | JavDB 接口补齐 | 工作线 D（排行、评论、实体详情） | 待做（番号容差已于 2026-09-23 提前落地） | M2 |

下一步：实施 M7（工作线 D：JavDB 接口补齐 - 排行、评论、实体详情）。

---

## 8. 验证与回归边界

- 每个提交：`gofmt -l` 为空、`go test ./...`、`go vet`、前端 `npm test`、`tsc --noEmit`、oxlint、Vite 构建。间歇失败视为红。
- 哨兵错误跨包搬家时，必须有一条测试从最底层的返回值一路断言到 HTTP 状态码；只改类型不跑链路会像 M5 的转码错误那样静默退化成 500。
- `-race` 需要 cgo；Windows 开发机默认 `CGO_ENABLED=0` 跑不了，在 Linux（Docker 构建镜像或 WSL）上跑 `go test ./... -race`，至少每个里程碑收口时跑一次。
- M2 起：黄金 JSON 测试证明所列端点响应逐字节一致。
- M3：`drive` 并发测试覆盖登出、换目录、令牌刷新中途发生三类场景（已迁入 `internal/drive`）。
- M4：e2e 测试在每个包迁出后重跑；`internal/service` 删除时 e2e 仍通过（已验证，`9b4d21f`）。
- M4 起：`internal/app/deps_test.go` 锁定依赖方向，业务包之间、`drive / tasks / database` 对业务包不得出现 import。
- M6：JavBus 固件测试；聚合器测试覆盖单源超时、单源失败、重复 infohash 合并、`Inferred` 标记、排序稳定性；Picker 测试锁定"站方字幕不因名称含其它推断标记而降权"；订阅测试覆盖重复添加、演员游标基线与回填、手动订阅不推送、批量任务计数与任务列表投影。黄金测试 `discover_magnets.json` 含 `sources / tags / inferred`。
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
- 订阅表（2026-09-22，`ccf1750`）：`monitors` 在 B6 更名 `subscriptions` 并按 4.5 扩字段，旧行以 `kind=movie、auto_download=1` 迁入后删表；`/api/monitors*` 路径与响应暂不变，前端零改动，换 `/api/subscriptions` 随工作线 C。
- 周期任务（2026-09-22）：离线同步与订阅检查两个循环统一为 `tasks.RunPeriodic`，由 `app.Run` 启动；`monitor` 通过 `Pending()` 通道提前唤醒。
- 业务包边界（2026-09-22）：业务包只通过 `domain` 类型或调用方定义的接口交互（`offline.Catalogue / TargetedScanner`、`monitor.Discoverer / OfflineAdder`、`maintenance.ArtworkLocker`、`catalogue.Provider / LocalState`）。跨包 DTO 进 `domain`，跨包 ent 查询范围进 `database`，不在业务包之间 import。规则由 `app/deps_test.go` 强制。
- 别名（2026-09-22）：迁包时留下的 17 个 `type X = Y` 兼容别名全部删除，后续迁移一律直接改引用，不留别名。

**网络与代理**

- 一个开关一个地址；开启时 JavDB 与 JavBus 走代理，115 永远直连。JavBus 只有 `https://www.javbus.com` 一个端点，内置为包常量，不做镜像与自定义地址。
- `MIYABI_PROXY` 彻底删除，不作初始种子；代理密码不脱敏；校验错误中文化并映射 400（`domain.KindInvalid`）。
- `Normalize` 在地址为空时把 `Enabled` 归一为 false 并持久化，"开启但无地址"不是合法状态。
- `golang.org/x/net/html` 提升为直接依赖（工作线 C）。

**产品**

- JavBus 数据默认关闭，设置页独立"JavBus"分区一个开关（`javbus.enabled`，`GET/PUT /api/settings/javbus`），开关归 `catalogue`，运行时以原子布尔门控聚合源并清空磁力缓存；磁力按来源加徽章。
- 磁力推断（2026-09-22 收口）：站方标注与名称推断分开记录，Picker 对推断属性减半计分；`4K` 视为推断高清，`无码 / 破解 / 流出` 永远是推断。
- 追踪改为订阅，支持影片与演员；影片与演员共用一个每日检查时间（默认 04:00），设置页"订阅设置"分区改动即保存，不设体积上限；独立路由 `/subscriptions` 进 `FloatingNav`；单部、多选、一键入库，批量走任务队列并进入任务列表与 toast。
- 订阅重复添加（2026-09-22 收口）：用户重复添加已入库或过期的影片视为重新订阅并重置；演员检查派生的添加绝不重置已有订阅。演员订阅以订阅时的作品页为游标基线，之后只把未见过且不早于水位线一个月的作品当新作，避免游标失效时把旧作全部推入 115。
- `config.Runtime`（2026-09-22）：不新增环境变量，对外配置维持四个。Runtime 只放组合根真正注入的常量，字段只在有消费者时才存在；上游客户端自己的超时与限速留在各自包内命名常量。
- 审计文档已删除（`badcfa5`）。

**前端样式例外**（"原样沿用"约束的三次例外，3.9 清单以此为新基线）

- 徽章（2026-09-21，`23c58cc`）：Badge 新增 `library`（紫色，已入库/新入库）与 `frosted`（磨砂，番号/下载中）两个变体，"预览"文案改为"有预览"。
- 分页器（2026-09-21，`562f49d`、`58622d6`、`1b64d52`、`09e1b6f`）：`ListPagination` 改为 shadcn `PaginationLink / PaginationEllipsis` 页码链接，库页面移除"共 N 部影片 · 每页 20 部"文案，单页时隐藏。页码算法在 `lib/pagination.ts`：连续窗口 3 页（当前页 ±1），首尾页始终可点，总页数 ≤7 时全列，省略号不用于只遮一页。当前页 `aria-current="page"` 不可点，禁用态 `aria-disabled` + `pointer-events-none`；上一页/下一页保持原生 `Button`。传 `totalPages` 的页面（库、观看历史）渲染完整页码，发现页只渲染当前页占位。`562f49d` 的页码输入框已被页码链接替代。
- 订阅页（2026-09-22，M6 收口）：新页面只组合现有 `MovieCard / Badge / Button / Checkbox / Avatar / Switch / Tooltip / Dialog / Tabs / Skeleton`，卡片选择态沿用历史页的 `ring-2 ring-success` 与右上角 `Checkbox`；演员条目是 `rounded-2xl border p-2` 容器内的 `Avatar` 加 `Button`，没有新增 Badge 变体与动效。每日检查时间用 shadcn `Input type="time"` 并隐藏原生日历指示器。
- 番号格式等价与补零容差（2026-09-23）：在 `internal/codeid` 新增 `IsFormatEquivalent` 与 `UnpaddedNumericCandidate`，纯字符结构与算法推导，无硬编码字典；`ResolveMovieID` 实行严格全等优先、格式等价兜底、去零退避搜索，多等价候选严格拒绝防串片。
- 字幕引擎与自动化播放（2026-09-23，M9）：跨源聚合（迅雷云字幕 API + SubtitleCat HTML 解析），基于 15,000 字符简繁特征频次加权识别中文；自动转码为标准 WebVTT 并支持毫秒级时间轴偏移；本地 115 目录同级字幕自动索引关联；刮削完成自动触发在线最优字幕下载、转码、回传 115 媒体文件夹与本地缓存；播放器控制栏采用参考 jm-boom 风格的磨砂暗黑下拉菜单（`DropdownMenu`），支持字幕轨即时切换、±0.5s/±1.0s 微调持久化与在线候选热检索加载。

