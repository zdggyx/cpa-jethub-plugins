# 版本管理与上游同步

本文件规定本 fork 的版本标识、分支模型、上游同步、产物哈希与回退规则。部署操作细节见 `agent.md` 第 4 节。

## 1. 版本标识

- **上游版本**：以上游 git tag 为准（当前基线 `v0.11.0`，commit `8e9ce9adb59c5dfe621b56379ace5c4418514d71`）。
- **本地标签**：`v<上游版本>+local.<主题>.YYYYMMDD`，例如 `v0.11.0+local.trae-solo-tools-catalog.20261008`。
- 插件二进制内部版本常量固定 `0.1.0`，不随发布变化；实际识别版本以 nas `versions.json` 的 `build` 字段与产物哈希为准。

## 2. 分支模型

| 分支 | 用途 | 规则 |
| --- | --- | --- |
| `main` | 上游镜像 | 不直接提交；只用 `git fetch upstream --tags` 对齐 |
| `local/patches` | 本地改动唯一分支 | 每个上游版本 rebase 一次；所有本地修复按主题分 commit |

- `origin` = `https://github.com/zdggyx/cpa-jethub-plugins`（本 fork）。
- `upstream` = `https://github.com/collegeming/cpa-jethub-plugins`。

## 3. 上游同步步骤（每次上游发版）

1. `git fetch upstream --tags`
2. `git checkout local/patches && git rebase --onto v<新版本> v<旧版本>`
3. 预期冲突文件集中在本地补丁涉及处（TRAE：`config.go` / `models.go` / `plugin.go` / `pluginui.go`；`translate.go` 与 `executor.go` 历史上未被上游改动）
4. 容器内 `go test ./...` 全绿；补丁自带回归测试不得删改
5. 更新本地标签（日期与主题）、`CHANGELOG.local.md`、nas `versions.json`
6. 构建（`agent.md` 第 3 节）→ 部署与验收（`agent.md` 第 4 节）

## 4. 产物与哈希

- 构建产物：`dist/linux/amd64/<id>-v0.1.0.so`，校验和文件 `dist/linux/amd64/sha256sums.txt`。
- nas `configs/cliproxyapi/versions.json` 必录：
  - `plugins.build`：fork 地址、base tag/commit、local 分支/commit、本地标签、补丁文件与 sha256、SDK 版本、构建环境、工作区路径；
  - `plugins.installed`：8 个装机插件的 sha256；
  - `plugins.previous`：被替换的前一版本与备份目录。
- 装机集合固定 8 个：`atomcode`、`codebuddy`、`hub`、`minimax`、`qoder`、`trae`、`workbuddy-cn`、`zcode`；新增/移除装机插件属于架构决定，需用户确认。

## 5. 回退

- 插件回退：`/root/backup-cpa-<日期>-<标签>/plugins.before` 整目录换回 + 重启；
- CPA 主程序回退：同目录 `CLIProxyAPI.before` 换回；
- 回退完成后，nas `versions.json` 与 `部署日志.md` 必须记录回退事实。

## 6. 本地改动清单（截至 2026-10-08）

- **TRAE**（来源：credits-panel `patches/cpa-trae-solo-tools-catalog.patch`，sha256 `57582eeba41720171aebbfc468f9232c33966fa417dceee61ff26b515e8b5dbb`，基线 v0.9.0 → 已 rebase 至 v0.11.0，commit `15a1a39`）：
  - SOLO 工具分片累积修复（按 index/id 合并、稳定首片、拒绝无名续片）；
  - 文本尾部守卫 `text_tail_guard`（仅 `deepseek-v4.1-flash` 非约束文本，缺尾标记返回 `length`）；
  - 模型目录过滤：仅 `solo_agent` / `solo_work_lite` / `solo_agent_remote` 三通道（验收时 20 个模型）。
