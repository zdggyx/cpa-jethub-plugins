# cpa-jethub-plugins（zdggyx fork）项目规则

本文件是本 fork 仓库的长期治理文档。开始改动前先阅读本文件，并用当前代码、`git status`、NAS 部署记录核对事实；历史叙述不能替代当前验证。

## 1. 项目边界

- 上游：`collegeming/cpa-jethub-plugins`。本 fork 只维护少量本地改动（TRAE 补丁、后续平台适配与工具调用修复），不整体改写上游实现，不做与修复无关的重构。
- 用途：把 Jet-Hub 的平台适配器以 CPA 原生插件（`-buildmode=c-shared` 动态库）形式部署在 NAS `zy` 上，由 CPA 宿主统一提供网关能力。
- 每个插件目录是单一 `package main`：`main.go` 导出 4 个 C 符号，`plugin.go` 返回注册表与方法表，其余文件承载业务。
- 插件自身不建立网络连接；HTTP 一律经宿主 `host.http.*` 执行。

## 2. 分支与版本

- `main`：上游镜像，只用于 `git fetch upstream` 对齐，不直接提交。
- `local/patches`：唯一本地改动分支，基线为上游 tag（当前 v0.11.0）+ 本地 commit。
- 本地标签：`v<上游版本>+local.<主题>.YYYYMMDD`（当前 `v0.11.0+local.trae-solo-tools-catalog.20261008`），必须同步写入 nas `versions.json` 与本仓库 `CHANGELOG.local.md`。
- 上游发新版后的同步步骤见 `docs/VERSIONING.md`。

## 3. 构建与产物

- 本机是 macOS arm64：构建必须 `docker run --platform linux/amd64 golang:1.26-bookworm`，`GOOS=linux GOARCH=amd64`、`CGO_ENABLED=1`；arm64 容器里 cgo 交叉会报 `gcc: unrecognized command-line option '-m64'`。
- 产物在 `dist/linux/amd64/`，命名 `<id>-v<内部版本>.so`（内部版本常量固定 0.1.0；文件名决定插件 ID）；构建后生成 `sha256sums.txt`。
- 验证：容器内 `go test ./...` 全绿才算通过；行为修复必须带最小回归测试，测试不得依赖真实平台凭据或网络。

## 4. 部署与验收（NAS zy）

- 流程：本机构建 → 打包 → 经 Tailscale scp → NAS 备份 `/root/backup-cpa-<日期>-<标签>/`（0700，配置类 0600）→ `install -o root -g root -m 0755` 替换 → 重启 `cliproxyapi`。
- 最低验收：journal 中 8 个插件 loaded/registered；`/v1/models` 含目标模型；改动渠道实测非流文本 + 工具往返；结果写回 nas 项目。
- 回退：插件换回备份目录并重启；回退后同步更新 `versions.json` 记录，保持记录与现场一致。
- 每次部署必须落档到 nas 项目：`configs/cliproxyapi/versions.json`、`部署日志.md`，必要时更新 `服务状态.md`。

## 5. 行为合同（插件侧）

- 执行器按上游协议声明入出格式（`chat-completions` / `anthropic`）；跨协议翻译由宿主完成，插件不重复实现。
- 凭据 JSON 字段名与 Jet-Hub TS 保持一致（对照 `docs/PORTING.md`）；不改凭据文件名推导规则。
- 工具调用：所有渠道必须真实下发 tools。**Qoder 加密路径不得把 tools 写死为空**；`tool_choice` 按上游可接受形态转换（CodeBuddy/WorkBuddy 系列只接受字符串，对象形式会被上游 400）。
- 模型命名双向：对外发统一名，发给上游用原生名；TRAE 模型目录过滤（`solo_agent` / `solo_work_lite` / `solo_agent_remote` 三通道）属于本地补丁，不得回退。
- 改动只落在目标插件；与任务无关的插件、auth 文件和宿主配置不动。

## 6. 安全

- 任何 token、key、凭据不得写入仓库、日志、提交信息或文档；NAS 备份目录中的敏感文件只留在 NAS。
- 不在插件里新增出网路径；配置解析保持宽松（单个坏值只损失它自己的默认值）。

## 7. 长期文档

- 上游文档（跟随上游演进）：`README.md`、`docs/ADDING-A-CHANNEL.md`、`docs/DEPLOYMENT.md`、`docs/PORTING.md`、`CHANGELOG.md`。
- 本 fork 治理文档：本文件、`docs/VERSIONING.md`、`CHANGELOG.local.md`。
- 部署事实记录在 nas 项目（`~/xiangmu/nas`），不在本仓库重复。

## 8. 当前工作与待办

- [ ] 修复 Qoder 工具调用：加密路径按上游 `buildQoderTools` 下发 tools（当前 `payload.go` 写死 `Tools: []any{}`）。
- [ ] CodeBuddy/WorkBuddy：`tool_choice` 对象形式转字符串，修复上游 400。
- [ ] 缓存验证：OpenCode 侧历史观察"调用无缓存"；逐渠道核对 usage 缓存字段与请求透传。
- [ ] 跟进宿主 `/v1/models` 别名重复（带/不带前缀、AtomCode 双前缀，见 nas 部署日志 2026-10-08）。
- 背景：Mac 的 OpenCode 后续会退役相关 API，这批模型将在新壳子（Hermes）中启用；验收时需兼顾该路径。
