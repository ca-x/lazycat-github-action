# LazyCat GitHub Action

[English](README.md)

`ca-x/lazycat-github-action` 用于检查 Docker 镜像版本、精确更新 LazyCat Manifest、构建 LPK、创建更新 Pull Request，并把校验后的 LPK 上传到 GitHub Release。

Action 使用 [`github.com/lib-x/lzc-toolkit-go`](https://github.com/lib-x/lzc-toolkit-go) `v0.3.3`，兼容基线是 `@lazycatcloud/lzc-cli` `2.0.8`。

当前交付范围：

- Milestone 1：静态 Web 和 Exec 构建、LPK 校验、SHA256、amd64 和 arm64 Action 二进制。
- Milestone 2：stable、beta、nightly 和 custom OCI 检查；LazyCat、direct 和 mirror 镜像交付；Pull Request；Artifact；tag；Release；Release Asset。
- Milestone 3：懒猫官方开发者平台提交、喵喵私有商店提交、完整源码构建示例和仓库内 Agent Skill。

## 选择使用方式

两个公开入口都受支持，并共同跟随浮动的 `v1` 发布标签：

| 入口 | 引用方式 | 适用场景 |
|---|---|---|
| Composite Action | `ca-x/lazycat-github-action@v1` | 现有 job 已经负责 checkout、权限、工具链安装和 GitHub 写操作。 |
| Reusable Workflow | `ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1` | 需要完整 LazyCat CI/CD，包括工具链、Pull Request、Artifact、tag、Release、Asset 和商店发布。 |

一般 CI/CD 推荐调用 reusable workflow：

```yaml
jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      config: .github/lazycat-action.yml
    secrets: inherit
```

也可以在现有 job 中直接调用 composite Action：

```yaml
- uses: ca-x/lazycat-github-action@v1
  id: lazycat
  with:
    operation: build
    version: ${{ github.ref_name }}
```

调用方不需要编译本项目。启动脚本会按 Runner 架构下载 Action 二进制，并校验发布包 SHA256。

## 进度日志

Action 使用 Go 官方 `log/slog` 输出结构化进度，同时不会打印 Secret 值和受保护的构建环境变量。每次运行会先显示执行模式（`docker-image`、`source-build`、`prebuilt-content` 或 `store-publish`），再按实际流程输出以下阶段：

- Docker 版本查询、候选数量、选中的 Tag/版本/digest/平台、镜像交付开始、节流后的 layer 进度和交付结果。
- LPK buildscript 开始、包组装、官方 lint，以及最终 LPK 路径、大小和 SHA256。
- 目标商店、已验证的发布文件、同版本跳过、提交开始和提交结果。

项目 buildscript 的 stdout/stderr 会实时透传，便于定位原生工具缺失等错误。Action 会显示进程退出码，但不会打印 buildscript 正文或受保护的环境变量。

## 使用 Skill

可以直接用自然语言要求 Agent，例如：“检查这个 LazyCat 仓库，创建同时发布两个商店的版本化 GitHub Release workflow，并保护 Go Template Manifest。”仓库 Skill 会检查 `package.yml`、`lzc-build.yml`、配置的 Manifest、工具链文件、`.gitignore`、Git 已跟踪的 `*.lpk` 和已有 `.github/` 内容；随后创建或更新 `.github/lazycat-action.yml` 与所需的 `.github/workflows/*.yml`，并报告所有变更文件、验证结果、未决问题和必需的 GitHub Secret 名称，但不会读取 Secret 值。

如果路径、镜像归属、策略、商店或工具链无法从仓库证明，Skill 会在生成项目文件前暂停确认。迁移历史 LPK 时，它先运行 `git ls-files '*.lpk'`，报告已跟踪文件数量和总字节数，并在删除前显示单独、醒目的 STOP。用户拒绝时保留全部文件；批准后只删除清点过的文件，并添加 `*.lpk` 和输出目录 ignore 规则。除非另行提出请求，否则绝不重写 Git 历史或回填旧 Release。

发布 workflow 会显式映射各已启用商店所需的 Secret，不再只依赖 `secrets: inherit`。组织 Secret 必须授权每个新加入的仓库；同名 Secret 的优先级为 Environment > Repository > Organization。

需要带版本号的 Release 文件时，设置 `versioned-release-asset: true`。原始已验证构建输出继续作为 validation Artifact，GitHub Release 使用 `<package-id>-v<version>.lpk`。私有商店接收已验证的 Release Asset URL 和 SHA256；官方商店上传同一份本地已验证 LPK 字节及 SHA256，但不会接收 GitHub Release URL。

Go Template Manifest 永远不会被执行或求值。独立的 `if`、`else`、`end`、`with`、`range` 控制行会连同缩进和 trim marker 被原样保护、恢复，内联表达式保持不变。marker 丢失/冲突、保护后 YAML 无效、目标歧义或模板意外变化时会 fail closed；完成前还会验证控制行和真实构建。

## Runner 架构和 LazyCat 目标架构

Action 的运行机器和 LazyCat 应用目标是两件事：

| 层次 | 支持值 |
|---|---|
| Runner 系统 | Linux |
| Runner CPU | amd64 或 arm64 |
| LazyCat 目标系统 | Linux |
| LazyCat 目标 CPU | 由 `project.target_arch` 配置；默认 amd64，可选 arm64 |
| OCI 检查和复制平台 | 与项目目标一致的 `linux/amd64` 或 `linux/arm64` |

ARM64 self-hosted Runner 使用 ARM64 版本的 Action 二进制，但 buildscript 仍然收到：

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=<project.target_arch>
LAZYCAT_TARGET_PLATFORM=linux/<project.target_arch>
```

reusable workflow 支持传入 Linux Runner 标签：

```yaml
jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      runner: self-hosted-linux-arm64
      config: .github/lazycat-action.yml
    secrets: inherit
```

上面的标签只是示例，需要在 self-hosted Runner 上自行配置。切换 Runner 不会把 LPK 目标改成 ARM。

## 基本概念

- `package.yml` 保存 package ID、版本、显示信息和 locales。
- `lzc-manifest.yml` 保存应用路由，以及可选的 application 或 service 镜像。
- `lzc-build.yml` 指向 Manifest、内容目录和可选的项目 `buildscript`。
- `.github/lazycat-action.yml` 告诉 Action 它负责哪个版本来源和哪些镜像目标。
- Workflow Artifact 是 GitHub Actions 保留的 CI 产物。
- Release Asset 是挂在 GitHub Release 下的公开版本文件。

Action 默认执行基础 LPK lint。设置 `stores.official.enabled: true` 后会执行懒猫官方 lint profile，同时要求所有受管运行时镜像使用 `delivery.mode: lazycat`。

## Docker 镜像应用快速开始

假设应用有数据库服务 `db` 和真正对外显示页面的 Web 服务 `web`：

```yaml
# lzc-manifest.yml
application:
  subdomain: example
  routes:
    - /=http://web:8080/

services:
  db:
    # upstream: postgres:17
    image: registry.lazycat.cloud/acme/postgres:copy-id
  web:
    # upstream: ghcr.io/acme/example-web:v1.0.0
    image: registry.lazycat.cloud/acme/example-web:old
```

Action 不会猜测 `web` 是主服务，需要显式配置两个不同职责：

- `update.version_source.image: web` 表示使用 `web` 的镜像版本更新 `package.yml.version`。
- `images[].target: service` 和 `service: web` 表示 Manifest 编辑器只能修改 `services.web.image`。

`db` 已经使用 LazyCat Registry 镜像，但没有出现在 `images` 配置中，因此这套自动化不会修改它。

创建 `.github/lazycat-action.yml`：

```yaml
version: 1

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/example.lpk
  target_arch: amd64

update:
  strategy: pull
  allow_downgrade: false
  version_source:
    type: image
    image: web

build:
  run_buildscript: true

images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/example-web
    channel: stable
    delivery:
      mode: lazycat

stores:
  official:
    enabled: true
    create_if_missing: false
    changelog_locales: [zh, en]
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
  private:
    enabled: false
```

`allow_downgrade` 默认为 `false`。版本来源镜像完成标签到 SemVer 的映射后，如果候选版本低于当前 `package.yml.version`，Action 会在复制镜像和修改文件前阻止降级。版本相同仍可刷新镜像引用或 digest。只有明确执行回退时才设置为 `true`。

把开发者平台 token 保存为 GitHub Secret `LAZYCAT_TOKEN`，`LZC_CLI_TOKEN` 是兼容回退名称。

再添加定时和手动触发 workflow：

```yaml
name: Check LazyCat images

on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

`strategy: pull` 是默认策略。发现新镜像后，workflow 只更新显式配置的目标，构建并校验 LPK，上传 Workflow Artifact，然后创建或更新 `lazycat/update-all`。

如果只想处理一个镜像，可以传 `image-id`：

```yaml
with:
  operation: check
  image-id: web
  config: .github/lazycat-action.yml
```

使用 `strategy: pull` 时，可以单独选择非版本来源镜像，Manifest 会更新，但 package 版本保持不变。direct publish 要求 `image-id` 指向配置的版本来源镜像，因为创建 GitHub Release 必须有新的应用版本。

## Channel 选择规则

| Channel | 选择规则 |
|---|---|
| `stable` | 默认选择最高正式 SemVer，也可显式使用 Docker Hub `updated` 排序 |
| `beta` | 默认选择最高预发布 SemVer，也可显式使用 Docker Hub `updated` 排序 |
| `nightly` | 在正则匹配结果中选择目标平台 OCI 创建时间最新的镜像 |
| `custom` | 使用正则过滤，并显式选择 `semver`、`created` 或 `updated` 排序 |

Stable 示例：

```yaml
channel: stable
tag_regex: '^v?\d+\.\d+\.\d+$'
exclude_regex: 'windows|arm64'
```

Beta 示例：

```yaml
channel: beta
tag_regex: '^v?\d+\.\d+\.\d+-(alpha|beta|rc|preview)\.'
```

Docker Hub 更新时间优先示例：

```yaml
channel: stable
sort: updated
tag_regex: '^v?\d+\.\d+\.\d+$'
```

`updated` 使用 Docker Hub 标签元数据 `last_updated`，与 OCI `config.created` 不同：重新指向或重新推送已有标签时，`last_updated` 可以变化而镜像创建时间不变。时间相同先比较映射后的 SemVer，再比较标签名。该模式需要显式启用、目前仅支持 Docker Hub，并且绝不会回退使用创建时间。如果最近更新的标签映射到更低版本，现有 `allow_downgrade: false` 保护仍然生效。

Nightly 示例：

```yaml
channel: nightly
tag_regex: '^nightly(-.*)?$'
```

nightly 版本由选中目标平台镜像的创建时间和 digest 生成，结果仍是合法 SemVer：

```text
0.0.0-nightly.20260710153020.a1b2c3d4e5f6
```

Custom 示例：

```yaml
channel: custom
sort: created
tag_regex: '^edge-'
version_regex: '^edge-(?P<version>\d+\.\d+\.\d+)$'
version_template: '{version}'
```

`version_template` 可以引用 `version_regex` 中的所有命名捕获组：

```yaml
version_regex: '^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$'
version_template: '{version}.{build}.0' # 20260603.01 -> 20260603.1.0
```

`version` 捕获组仍然必填。未知占位符或展开后不是合法 SemVer 时会直接失败。

镜像仓库发现使用 `github.com/google/go-containerregistry`。Action 会先应用 `tag_regex` 和 `exclude_regex`。SemVer 排序先按标签排名；`updated` 先按 Docker Hub 标签元数据排名，两者都按顺序检查 manifest，找到第一个可用的项目目标平台就停止。按创建时间排序因为目标镜像时间参与排名，仍必须检查全部候选 manifest。OCI index 和 Docker manifest list 只选择 `project.target_arch` 对应平台。默认降级保护可以防止旧版本映射静默降低应用版本。

## 镜像交付模式

### 复制到 LazyCat Registry

```yaml
delivery:
  mode: lazycat
```

Action 把选中的源镜像提交给懒猫开发者平台，并把 `Platform` 设置为 `project.target_arch`（默认 `amd64`，可选 `arm64`）。开发者平台执行远端 Registry-to-Registry 复制，返回最终的 `registry.lazycat.cloud/...` 地址。本地 Docker 不参与这次复制。

该模式需要 `LAZYCAT_TOKEN` 或 `LZC_CLI_TOKEN`。启用官方商店模式时只能使用这种交付方式。

### 显式镜像加速地址

```yaml
delivery:
  mode: mirror
  image_template: ghcr.1ms.run/acme/example-web:{tag}
  require_digest_match: true
```

Manifest 会使用展开后的镜像地址。模板支持 `{tag}`、`{digest}` 和 `{source}`。启用 `require_digest_match` 后，Action 会检查 mirror 中与项目目标平台对应的镜像，只有 digest 与源镜像一致才会修改 Manifest。

### 直接使用源镜像

```yaml
delivery:
  mode: direct
```

Manifest 直接使用选中的源镜像，Action 不执行复制。这个模式适合私有商店，或者明确依赖外部 Registry 的部署。

官方商店模式会拒绝 `direct` 和 `mirror`。这两种方式只用于非官方分发。

## Runner 是否需要 Docker

| 场景 | Docker 要求 |
|---|---|
| 检查公开 OCI tag 和 manifest | 不需要 |
| LazyCat 远端镜像复制 | 不需要 |
| direct 或 mirror 地址更新 | 不需要 |
| reusable workflow 登录私有源 Registry | 需要 Docker CLI；GitHub 托管 Linux Runner 已安装 |
| 项目 buildscript 自己构建 Docker 镜像 | 需要 |
| ARM64 Runner 执行 x64 Dockerfile 的 `RUN` 步骤 | 需要 Docker Buildx 和 QEMU |

只有项目 buildscript 需要 Docker 时才选择 Docker 工具链：

```yaml
with:
  toolchains: docker
  enable-qemu: true
```

私有源 Registry 检查可以配置以下 GitHub Secrets：

```text
REGISTRY=ghcr.io
REGISTRY_USERNAME=<username>
REGISTRY_PASSWORD=<token or password>
```

reusable workflow 使用 `docker/login-action` 写入 Docker 凭据，OCI 客户端会读取这些凭据。它们只负责 Action 侧的镜像检查。lzc-cli 2.0.8 对应的 LazyCat `CopyImage` API 没有源 Registry 用户名、密码或 token 字段，因此 `mode: lazycat` 使用私有源镜像时，还要确保开发者平台本身能够拉取该镜像。

## 认证

LazyCat 镜像复制和官方 LPK 提交按以下顺序解析认证信息：

1. `LAZYCAT_TOKEN`
2. `LZC_CLI_TOKEN`
3. `LAZYCAT_USERNAME` 和 `LAZYCAT_PASSWORD`，登录后得到只保存在内存中的 token
4. self-hosted Runner 上通过 `token-file` 显式指定的 token 文件

CI 推荐长期保存可撤销的 token。账号密码可以作为临时回退方式，但不建议把账户密码长期放在 GitHub Secrets；登录返回的 token 只在当前进程内存中使用，不写入磁盘。

本机已经通过 lzc-cli 2.0.8 登录时，lzc-cli 先读取 `LZC_CLI_TOKEN`，否则读取 `~/.config/lazycat/box-config.json` 的 `token` 字段。`lzc-cli config get token` 会打印当前生效的 token，不要在 CI 日志中运行。GitHub 托管 Runner 无法读取你的本机登录文件，必须把 token 配置为仓库或组织 Secret。

可信 self-hosted Runner 可以显式使用已有的 lzc-cli 兼容文件：

```yaml
with:
  token-file: ~/.config/lazycat/box-config.json
```

文件必须是普通文件，路径中不能包含符号链接，并且不能授予任何 group/other 权限。Action 不会自动继承开发机的本地登录状态。底层 API 示例见 [lzc-toolkit-go 认证文档](https://github.com/lib-x/lzc-toolkit-go/blob/main/README.zh-CN.md#例子五登录并提交-lpk)。

项目构建会执行仓库中的 `buildscript`。Action 会从 buildscript 环境中移除 LazyCat token、Registry 凭据、GitHub token，以及 GitHub output/control 文件路径。带写权限的发布 workflow 应只用于可信分支、tag、定时任务和手动运行，不要把继承的 Secrets 暴露给不可信 Pull Request 代码。

## Pull Request 和 Release 工作流

### 默认安全流程：先创建 PR，合并后发布

定时检查使用前面的 `strategy: pull` 配置。再添加一个默认分支 caller：

```yaml
name: Publish merged LazyCat update

on:
  push:
    branches: [main]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

更新 PR 合并后，默认分支 workflow 会重新构建 LPK。如果 `v<package version>` 还没有 Release，workflow 会创建 Release 并上传 LPK。同名 Release Asset 只有在 GitHub 返回的 SHA256 digest 一致时才会复用，digest 不同会直接失败。

### 直接发布

设置：

```yaml
update:
  strategy: publish
```

定时或手动镜像检查成功后，workflow 只提交受管的 package 和 Manifest 文件，commit 带 `[skip ci]`，然后推送当前分支、创建 `v<version>` 并上传 LPK。已有 tag 不会被移动；如果同名 tag 指向其他 commit，workflow 会失败。

直接发布会创建 Git commit、tag、GitHub Release 和 Release Asset。配置了商店时，reusable workflow 随后提交经过校验的 LPK。`strategy: pull` 永远不会发布商店。

## 商店发布

只有在 workflow 上传或安全复用 GitHub Release Asset，并确认 GitHub 返回的 SHA256 后，才会发布商店。没有 `services` 或 `images` 的静态 Web、Exec 应用同样使用这条发布链。

### 懒猫官方开发者平台

启用官方 lint 和发布：

```yaml
update:
  strategy: publish
  version_source:
    type: git

stores:
  official:
    enabled: true
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [zh, en]
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
    application:
      language: zh
      name: Example App
      source: https://github.com/acme/example
      source_author: acme
```

`create_if_missing: false` 只允许发布到已经存在的应用。允许创建时，`application.name` 默认读取 `package.yml.name`，`language` 默认为 `zh`。官方模式会执行与 lzc-cli 偏好一致的检查，包括 locales、图标不超过 200 KB、SemVer 元数据和 LazyCat Registry 运行镜像。`container_name` 等一般兼容性 warning 仍会展示，但不会阻断构建；只有被分类为官方商店 warning 的问题才会阻断官方发布，而且不会影响仅启用私有商店的 workflow。只要配置了 `direct` 或 `mirror`，就会在发布前失败。

`skip_if_version_exists: true` 会在 LPK 校验完成后，通过精确包名匿名查询官方商店。版本相同时返回 `published: false`、`skipped: true` 和 `skipReason: version-already-online`。两者均为合法 SemVer、线上版本更高且 `update.allow_downgrade: false` 时，也会安全跳过并返回 `skipReason: online-version-newer`；只有显式设置 `allow_downgrade: true` 才继续执行回退提交。non-SemVer 值只判断精确相等，绝不按字符串猜测顺序。跳过时不会解析开发者 Token，也不会提交 LPK。应用不存在时继续发布；其他查询错误直接失败。该选项默认 `false`，`dry-run` 仍然完全不发起远端请求。

官方发布始终把已验证的本地 LPK 文件作为 multipart 数据上传，绝不会把 GitHub Release URL 发送给官方平台。复用 Release Asset 时，会先把精确版本文件下载到项目目录下并重新校验。

官方重试为显式开启，默认 `enabled: false`。启用后，`max_attempts` 为 2-10 且包含首次尝试，`initial_delay` 与 `max_delay` 使用 Go duration 语法。审核前的安全重试会重新检查应用是否存在并重新打开 LPK，但凭据只解析一次。上传/检查阶段可重试无 HTTP 状态的连接/TLS/重置错误、HTTP 429 和 HTTP 5xx；审核创建只重试 HTTP 429。审核阶段的网络错误或 5xx 不会重放，因为服务端可能已经受理这个非幂等请求。取消、deadline 超时、鉴权、权限、NotFound、完整性错误、HTTP 400 和其他 4xx 都不重试。

失败会安全地区分 `store.official.upload` 与 `store.official.review`。Action 绝不打印原始响应正文；对合法 JSON 错误，只会显示经过单行化和长度限制的 `message`、`msg`、字符串 `error` 或嵌套的 `error.message`/`error.msg`，疑似凭据内容会被隐藏。双商店 reusable workflow 中，私有结果会被保留，官方失败降级为 warning，并写入 `store-results.official.failureReason: official-publish-failed`；如果官方商店是唯一目标，失败仍会使 workflow 失败。未启用官方商店时，不运行官方 lint 阻断、预检、凭据解析或发布。

reusable workflow 接受 `LAZYCAT_TOKEN`、`LZC_CLI_TOKEN`，或者 `LAZYCAT_USERNAME` 加 `LAZYCAT_PASSWORD`。推荐使用 token。

### 喵喵私有商店

应用元数据可以写入配置，凭据不要提交到仓库：

```yaml
stores:
  official:
    enabled: false
  private:
    enabled: true
    skip_if_version_exists: true
    name: Example App
    summary: Published from CI
```

配置 GitHub Secrets：

```text
APPSTORE_URL=https://store.example.com
APPSTORE_TOKEN=lcst_...
APP_ID=42
PRIVATE_STORE_GROUP_CODES=ABC123,LATE23
```

`APP_ID` 和 `PRIVATE_STORE_GROUP_CODES` 都是可选项。分组码属于访问凭据，必须以逗号分隔的 GitHub Secret 保存。它只用于匿名查询线上最新版本，由 toolkit 默认通过 `X-Group-Codes` 请求头发送，不会进入 Action inputs、outputs、summary 或结果 JSON。toolkit 会清除 Cookie jar 并禁止重定向，防止分组码被转发到其他来源。

启用 `skip_if_version_exists: true` 后，Action 会在读取 `APPSTORE_TOKEN` 前通过精确包名查询喵喵商店。相等版本和线上更高的 SemVer 分别使用 `version-already-online`、`online-version-newer`，并且每个商店独立判断；应用不存在时继续发布，其他查询错误直接失败。真正发布时，如果没有 `APP_ID`，写客户端会先按 `packageId` 精确查找，再用 `stores.private.name` 调用带 Token 的 `GET /api/v1/apps/by-name?name=...` 接口。商店只返回当前 Token 有权上传版本的唯一精确同名应用；404 时创建新应用，同名歧义或鉴权错误直接停止。按名称解析出的历史应用可以保留不同的 `packageId`，Action 只使用其数字 ID 追加新的外部版本。提供 `APP_ID` 时，仍会先确认该应用的 `packageId` 与 LPK 一致。

### Release/商店对账

定时或手动触发的 `publish` workflow 也会对账 GitHub Release 与两个商店。如果镜像检查没有文件变化，但当前版本缺少 Release 或精确的版本化 Asset，reusable workflow 会执行一次恢复构建，验证 LPK，并补建 Release/Asset。如果当前 Tag 已有精确命名的 `<package-id>-v<version>.lpk`，但某个商店还没有该版本，则下载到项目根目录下，同时校验 GitHub 返回的 `sha256:` digest 与本地重新计算的 SHA256，再用同一份字节补交。已经存在该版本的商店会独立跳过，workflow 绝不会猜测其他文件或版本。

### GitHub Secret 作用域和优先级

reusable workflow 只按名称读取 GitHub Actions Secret，不区分它来自组织还是仓库。组织级 Secret 必须通过 repository access policy 授权给当前仓库，否则工作流无法读取。

同名 Secret 同时存在于多个层级时，更具体的层级优先：Environment Secret 覆盖 Repository Secret，Repository Secret 覆盖 Organization Secret。例如仓库级 `APPSTORE_URL` 会覆盖组织级同名值。组织 Secret 适合提供多个仓库共享的默认值；只有确实需要单仓库覆盖时才创建仓库级同名 Secret。不要在多个层级重复定义同名 Secret，除非这是有意的覆盖关系。

新建应用调用 `POST /api/v1/apps`；已有应用的外部版本调用 `POST /api/v1/apps/{APP_ID}/versions`，两者都发送 JSON。`downloadUrl` 和确认过的 64 位小写 `sha256` 都是必填项。reusable workflow 会把 GitHub 校验过的 SHA 传给发布操作，发布操作重新计算本地 LPK，任何不一致都会失败。URL 必须是真实的 `https://github.com/<owner>/<repo>/releases/download/...` Release Asset 地址。私有商店可以直接记录 Action 提供的 checksum，不需要仅为了重新计算 SHA256 而下载 LPK。相同版本和 SHA256 会幂等返回已有结果；同版本内容不同会失败。

私有商店支持 Docker 的 `lazycat`、`direct`、`mirror` 三种模式，也支持完全没有 Docker 镜像的应用。`direct` 和 `mirror` 应用不能误发官方商店。

## 静态、Exec、Go、Rust 和 TypeScript 的 tag/release 构建

没有 Docker service 的项目使用 Git 作为版本来源：

```yaml
update:
  strategy: pull
  version_source:
    type: git
```

tag 触发和 release 触发二选一。同一个 tag 同时启用两种触发方式会构建两次。

Tag 触发：

```yaml
name: Build tagged LPK

on:
  push:
    tags: ["v*"]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      toolchains: go
    secrets: inherit
```

Release 触发：

```yaml
name: Build released LPK

on:
  release:
    types: [published]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      changelog: ${{ github.event.release.body }}
      toolchains: node
      node-package-manager: pnpm
    secrets: inherit
```

Action 会移除一个前导 `v`，更新 `package.yml.version`，运行项目 buildscript，构建并重新打开 LPK，执行 lint，计算 SHA256，然后上传到对应 Release。如果 tag/release checkout 修改了 `package.yml`，Release Asset 上传成功后，workflow 会把该文件同步回默认分支。

### TypeScript 静态 Web 构建

`lzc-build.yml`：

```yaml
buildscript: ./scripts/build.sh
contentdir: ./dist/content
```

`scripts/build.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail
npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
cp -R web-dist/. dist/content/
```

workflow 使用 `toolchains: node`，并传 `node-version` 或提交 `.node-version`。

如果 `.github/lazycat-action.yml` 同时声明了 `build.toolchains`，其中的工具链种类必须与 reusable workflow 输入一致。两边都显式填写版本时，版本也必须一致。

### Go Exec 构建

```bash
#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist/content
CGO_ENABLED=0 \
GOOS="${LAZYCAT_TARGET_OS}" \
GOARCH="${LAZYCAT_TARGET_ARCH}" \
go build -trimpath -ldflags='-s -w' -o dist/content/app ./cmd/app
```

workflow 使用 `toolchains: go`，并传 `go-version` 或在 `go.mod` 中声明 Go 版本。

### Rust Exec 构建

```bash
#!/usr/bin/env bash
set -euo pipefail
cargo build --release --target x86_64-unknown-linux-gnu
mkdir -p dist/content
cp target/x86_64-unknown-linux-gnu/release/example dist/content/app
```

workflow 使用 `toolchains: rust`。可以传 `rust-toolchain`，也可以提交包含 `toolchain.channel` 的 `rust-toolchain.toml`。reusable workflow 会安装 `x86_64-unknown-linux-gnu` 和 `aarch64-unknown-linux-gnu`；buildscript 根据 `LAZYCAT_TARGET_ARCH` 选择 triple，并自行提供需要的交叉链接器。

### Docker buildscript

```bash
#!/usr/bin/env bash
set -euo pipefail
docker buildx build \
  --platform "${LAZYCAT_TARGET_PLATFORM}" \
  --load \
  -t example-build:local .
```

workflow 使用 `toolchains: docker`。ARM64 Runner 的 Dockerfile 构建阶段需要执行 x64 程序时，保留 `enable-qemu: true`。

可直接复制的完整文件位于 [`examples/`](examples/)：

- [`docker-stable-lazycat`](examples/docker-stable-lazycat/.github/lazycat-action.yml) 和 [`docker-mirror`](examples/docker-mirror/.github/lazycat-action.yml)
- [`go-exec`](examples/go-exec/.github/workflows/lazycat.yml) 和 [`rust-exec`](examples/rust-exec/.github/workflows/lazycat.yml)
- [`typescript-static`](examples/typescript-static/.github/workflows/lazycat.yml) 和 [`typescript-exec`](examples/typescript-exec/.github/workflows/lazycat.yml)
- [同时发布官方和私有商店](examples/stores/.github/workflows/lazycat.yml)

TypeScript Exec 示例要求锁文件中包含 `@yao-pkg/pkg`，并用 `node22-linux-x64` 演示默认的 `amd64` 目标。TypeScript 静态资源通常与 CPU 无关；Go、Rust、TypeScript Exec 和 Docker 构建必须遵循 `LAZYCAT_TARGET_ARCH`/`LAZYCAT_TARGET_PLATFORM`，选择 arm64 的项目需要匹配的工具链和打包运行时。

## 静态和 Exec Manifest 可以没有 services

静态 Web：

```yaml
application:
  subdomain: example
  routes:
    - /=file:///lzcapp/pkg/content
```

Exec：

```yaml
application:
  subdomain: example
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

这类项目不需要 `images` 配置，版本直接来自 tag 或 release。

## Outputs

| Output | 含义 |
|---|---|
| `operation` | 最终执行的 `check`、`build`、`publish-official` 或 `publish-private` 操作 |
| `changed` | 受管项目文件是否变化 |
| `package-id` | LazyCat package ID |
| `package-file` | `package.yml` 绝对路径 |
| `manifest-file` | Manifest 绝对路径 |
| `version` | 去掉前导 `v` 的规范化 SemVer |
| `tag` | 规范化的 `v<version>` tag |
| `lpk-path` | 当前 job 中构建出的 LPK 绝对路径 |
| `sha256` | 64 位小写 LPK SHA256 |
| `download-url` | 发布后确认过的 GitHub Release Asset URL |
| `image-results` | 镜像选择和交付结果 JSON 数组 |
| `store-results` | 官方和私有商店发布结果 JSON 对象 |
| `official-store-enabled` | 配置是否启用官方商店 |
| `private-store-enabled` | 配置是否启用私有商店 |
| `update-strategy` | `pull` 或 `publish` |
| `channel` | 驱动应用版本的镜像 Channel |
| `result-file` | 完整且不含秘密的 JSON 结果文件 |
| `runner-arch` | `amd64` 或 `arm64` |
| `target-platform` | 默认 `linux/amd64`；设置 `project.target_arch: arm64` 时为 `linux/arm64` |

`image-results` 单项示例：

```json
{
  "id": "web",
  "target": "service",
  "service": "web",
  "platform": "linux/amd64",
  "tag": "v2.0.0",
  "sourceRef": "ghcr.io/acme/example-web:v2.0.0",
  "sourceDigest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "deliveryMode": "lazycat",
  "deliveredRef": "registry.lazycat.cloud/acme/example-web:copy-id",
  "copied": true,
  "copyResult": {
    "sourceImage": "ghcr.io/acme/example-web:v2.0.0",
    "platform": "amd64",
    "lazyCatImage": "registry.lazycat.cloud/acme/example-web:copy-id",
    "finished": true
  }
}
```

完整结果写入 `.lazycat-action/result.json`。token、密码、Cookie 和 Authorization 请求头不会写入 outputs 或 step summary。

`store-results` 示例：

```json
{
  "official": {
    "published": true,
    "skipped": false,
    "created": false,
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "uploadUrl": "/developer/uploads/example.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "private": {
    "published": true,
    "skipped": false,
    "created": false,
    "existing": false,
    "appId": "42",
    "versionId": "56",
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "downloadUrl": "https://github.com/acme/example/releases/download/v1.2.3/app.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}
```

发现相同线上版本时，对应商店结果改为 `published: false`、`skipped: true`，并且 `version` 与 `onlineVersion` 相同；不会读取写入凭据或调用提交接口。

## Artifact 和 Release Asset 的区别

- 只要本次运行生成了 LPK，就会上传 Workflow Artifact，便于 CI 检查。
- Pull Request 模式到 Artifact 和 PR 为止。
- Release 流程还会把 LPK 挂到 GitHub Release，并返回 `download-url`。
- 私有商店发布使用确认过的 Release Asset URL 和本地 SHA256，让商店直接信任提供的 digest，不必为了重新计算而下载文件。

## Dry run

```yaml
with:
  operation: check
  config: .github/lazycat-action.yml
  dry-run: true
```

Dry run 会选择版本并返回计划中的镜像地址，但不会复制镜像、修改文件、运行 buildscript、创建 PR 或创建 Release。

完整目标行为见[设计规格](docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md)。
