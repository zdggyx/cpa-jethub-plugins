# 本地变更记录（fork: zdggyx/cpa-jethub-plugins）

只记录本 fork 相对上游的变更；上游本身的变化见 `CHANGELOG.md`。每次部署的现场证据（备份目录、哈希、验收）记录在 nas 项目 `部署日志.md`。

## 待办（未完成）

- 缓存验证：核对各渠道 usage 缓存字段与请求透传（OpenCode 侧历史观察"调用无缓存"）。
- 跟进宿主 `/v1/models` 别名重复（带/不带前缀、AtomCode 双前缀）。
- 背景：Mac 的 OpenCode 后续会退役相关 API，这批模型将在新壳子（Hermes）中启用。

## 2026-10-08（下午）— 工具调用修复：Qoder 工具下发 + CodeBuddy tool_choice

- `fix(qoder)`（commit `cdc618d`）：加密载荷真正下发顶层 `tools`（上游 `76069ce` 同源问题的 Go 移植）；解析 wire 的 tools / assistant `tool_calls` / tool `tool_call_id`，空 description/parameters 不写键，`arguments` 保留原始字符串；新增 `payload_tools_test.go` 四组回归。
- `fix(codebuddy)`（commit `ba52043`）：对象形态 `tool_choice` 翻译为字符串（function/tool 对象 → `required`，其他对象/标量 → `auto`，字符串原样通过）；新增五种输入形态的回归测试。
- 构建部署：qoder / codebuddy / workbuddy-cn 三个产物替换到 NAS，label `v0.11.0+local.tools-fix.20261008`；NAS 备份 `/root/backup-cpa-20261008-tools/`。
- 线上验收：Qoder `01a0bb44/qfmodel` 工具往返通过（修复前 finish=stop、无 tool_calls、正文臆造 XML）；WorkBuddy 对象 tool_choice 从上游 400 变为 200 且工具往返通过。
- 落档：nas `versions.json`、nas `部署日志.md`（2026-10-08 下午节）。

## 2026-10-08 — fork 建立、v0.11.0 基线、TRAE 补丁 rebase 与首次部署

- 建立 fork `zdggyx/cpa-jethub-plugins`；本地工作区 `/Users/zhao/xiangmu/cpa-jethub-plugins`（origin=fork，upstream=collegeming）。
- 基线 `v0.11.0`（`8e9ce9a`）；本地改动分支 `local/patches`，首个 commit `15a1a39`。
- TRAE 补丁从 v0.9.0 基线干净 rebase 到 v0.11.0（仅行号偏移，无冲突）：SOLO 工具分片、`text_tail_guard`、模型目录过滤；`go test ./...` 全绿。
- 建立本机构建链路：`docker run --platform linux/amd64 golang:1.26-bookworm`，15 个产物（11 插件 + 4 变体）。
- 部署 NAS：CPA `8.0.12 → 8.0.20` 同步升级，8 个插件全部替换为本 fork 构建；备份 `/root/backup-cpa-20261008-v011-v8020/`。
- 验收：AtomCode GLM / WorkBuddy DeepSeek / TRAE DeepSeek 精确回复通过；AtomCode 工具往返（19+23=42）通过。
- 观察：宿主 `/v1/models` 出现别名重复（110 条；带/不带前缀，AtomCode 双前缀），插件自报目录干净，三种形态均可路由；列为待跟进。
- 落档：nas `configs/cliproxyapi/versions.json`、nas `部署日志.md`（2026-10-08 节）。
