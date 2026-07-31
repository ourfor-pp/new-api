---
name: manage-new-api-fork
description: 维护 ourfor-pp/new-api Fork 与 QuantumNous/new-api 上游关系。用于同步或评估上游版本、创建功能或修复分支、整理提交、创建或合并 GitHub PR、生成候选版本、处理冲突，以及判断应合并、重写还是暂缓社区改动。
---

# 管理 new-api Fork

## 开始前

1. 读取仓库根目录 `AGENTS.md`。
2. 读取 [references/repository-relationship.md](references/repository-relationship.md)。
3. 执行只读检查：
   - `git status --short`
   - `git remote -v`
   - `git branch -vv`
   - `git log --oneline --decorate -n 20`
4. 保护用户已有修改；工作区不干净时先划清改动归属，不得用 reset、checkout 或覆盖式操作清理。

## 选择工作流

### 日常功能或修复

1. 从当前 SXH 稳定分支新建 `feature/*` 或 `fix/*`。
2. 保持业务模型、生产配置和密钥与代码分离。
3. 完成代码、定向测试、全量回归和差异检查。
4. 使用 `.github/PULL_REQUEST_TEMPLATE.md` 创建 PR，目标分支为对应的 `sxh/*`，不是上游分支。
5. 合并后同步本地稳定分支，再生成候选镜像或发布记录。

### 同步上游版本

1. 只执行 fetch，先比较标签、迁移、配置、API、前端依赖和数据库兼容性。
2. 生成上游变更清单，并突出 SXH 相关模块：
   - 渠道路由、重试、自动禁用和渠道测试；
   - 模型映射、模型定价、计费和日志；
   - 火山语音适配；
   - 数据库迁移、Redis、多实例和定时任务；
   - Docker、前端构建和环境变量。
3. 不直接重写现有稳定分支。为新上游基线创建新的升级分支，在新标签上选择性重放或重写 SXH 提交。
4. 社区 PR 只作为设计输入。协议、计费或可靠性语义与当前代码不一致时，按当前基线重写，不盲目 cherry-pick。
5. 通过完整验证和真实兼容性演练后，才能建立新的 `sxh/<upstream-version>` 稳定线。

## GitHub 规则

- `origin` 是 SXH 可写 Fork；`upstream` 只用于读取，不向其推送。
- 创建 PR 前比较当前 Git 身份与历史核心作者；当前维护身份不是上游核心作者时，在 PR 模板中明确说明 AI 辅助。
- 优先使用已配置的 GitHub 工具；没有 `gh` 时可使用 GitHub API，但不得打印、写入文件或提交 Token。
- PR 必须包含变更目标、兼容性、测试结果、生产边界和回滚影响。
- 未经用户授权，不部署生产、不修改渠道、不改数据库。

## 候选版本

1. 从已合并且工作区干净的提交生成，不从未提交工作区构建正式候选。
2. 更新 `VERSION` 和 `docs/releases/`，版本号不得复用。
3. 镜像标签同时包含候选版本和短提交号。
4. 记录 Go、前端、race、Docker 和真实联调的验证边界；依赖下载故障不得伪装成代码失败。
5. 将“代码合并”“候选镜像”“生产部署”作为三个独立状态报告。
