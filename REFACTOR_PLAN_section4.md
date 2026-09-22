## 4. 工作线 C：磁力聚合与订阅（已完成，2026-09-22，提交 `217e28b`、`e0de46b` 及收口）

### 4.1 为什么只要 JavDB + JavBus

两者都是人��维护的聚合站，磁力经过筛选并带站方标注的"高清 / 字幕"标签，质量远好于 Sukebei 这类原始索引；收录互补（JavBus 常先收录 DMM 系新片，旧片保留更久），合并去重后覆盖面明显提升。两者都以番号为键，不需要身份映射。

### 4.2 `internal/javbus`

- **端点**：只有 `https://www.javbus.com` 一个，���置为包常量；没有镜像，不提供自定义地址。
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

**设置**（`subscription.config`，设置页"订阅设置"分区，改动即���存）：影片默认自动入库（默认开）、演员新作默认自动入库（默认关）、每日检查时间（`HH:MM`，默认 04:00，影片与演员共用）、磁力偏好三项（默认字幕优先、高清优先、无码不限）。写入经 `Config.validate`，非法值 400。

**检查**（`tasks.RunPeriodic`，`MonitorCheckInterval` 5 分钟一轮，只处理到期项）：
- 影片：未发售每三天、发售当天起每天一次，都落在每日检查时间；发售 30 天后仍无磁力转 `stale`。取聚合磁力经 Picker 选一条；`auto_download` 开则推 115 转 `added`，关则记下 `hash` 等用户入库并次日再查。上游失败一小时后重试，错误文案写入 `error`。
- 演员：每天一次取 `Browse(actor, release desc, 40)`。订阅时以该页做游标基线，只对未发售作品立即建影片订阅（继承 `auto_download / zone`，`origin_id` 指向演员）；之后每次把"未见过且发行日期不早于水位线减 30 天"的作品当新作派生订阅，避免游标丢失时把旧作全部推入 115。派生添加绝不重置已有订阅；用户手动重复添加已入库 / 过期的影片才重置。`Browse` 失败时不创建演员订阅。已见列表保留最新 300 个 id。

**入库**：`POST /api/subscriptions/:id/enqueue` 单部：Picker 选中即推 115 转 `added`；无合格磁力则保持 `waiting` 并置 `auto_download=true`；已 `added` 直接返回不重复推送；演员订阅返回 400。`POST /api/subscriptions/enqueue {ids | all}` 创建 `subscription_batch` 任务由单 worker 顺序处理，每部间隔 1.5 到 3 秒随机；payload 记录 `total / processed / submitted / waiting / failed / failures[]`（最多 20 条明细），`tasks.ListWorkflows` 把最近 5 条与进行中的批量任务并入 `/api/tasks`，前端 `TaskNotifications` 以 toast 展示进度与失败明细。空选择返回 400。

**API**：`GET /api/subscriptions?kind=&page=&limit=`、`POST /api/subscriptions {kind, target_id, title?, cover?, auto_download?, zone?}`、`PATCH /api/subscriptions/:id {auto_download?, zone?, status?}`（`status` 只接受演员的 `active / paused`）、`DELETE /api/subscriptions/:id`、上面两个 enqueue、`GET /api/subscriptions/actors/:id/feed`、`GET/PUT /api/settings/subscription`。`/api/monitors*` 已删除。

**前端**：`FloatingNav` 新增"订阅"（`Bell`），路由 `/subscriptions`，`Tabs` 两页。影片订阅页复用 `MovieGridLayout / MovieCard`，状态徽章沿用 `Badge` 变体，`MovieStateBadge` 让已入库影片不显示"入库"按钮；选择模式与"全选待入库 / 入库选中 / 一键入库"照搬历史页（卡片包 `Button` 加右上角 `Checkbox`，选中 `ring-2 ring-success`，确认用现有 `Dialog`）。演员订阅页上方是可横向滚动的演员条目（`Avatar` + 名称 + 新作数 + 自动入库 `Switch` + 暂停 / 取消），点选后网格只显示其新作，未选中显示全部派生新作，网格与影片页共用同一组件。演员作品页头部有"订阅演员"，详情页与即将发行卡片的追踪按钮改为"订阅"。
