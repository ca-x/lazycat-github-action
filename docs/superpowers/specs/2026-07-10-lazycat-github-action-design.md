# LazyCat GitHub Action 设计规格

日期：2026-07-10

仓库：`github.com/ca-x/lazycat-github-action`

核心依赖：`github.com/lib-x/lzc-toolkit-go v0.1.0`

兼容基线：`@lazycatcloud/lzc-cli 2.0.8`

## 1. 目标

本项目提供一个可直接在 GitHub Workflows 中使用的 GitHub Action，用于 LazyCat LPK 项目的持续更新、构建、校验、发布和商店提交。使用者不需要安装 Go，也不需要编译本 Action：

```yaml
- uses: ca-x/lazycat-github-action@v1
```

项目同时提供一个 reusable workflow，封装 PR、GitHub Release、Artifact 和商店发布等 GitHub 平台编排。核心 Go Action 负责确定性业务逻辑；成熟的第三方 Action 负责 GitHub 平台写操作。

首个大版本支持以下场景：

- 定时检查 Docker 镜像的 stable、beta、nightly 或自定义版本；
- 将选中的镜像精确更新到指定 service 或 application；
- 将源镜像复制到 LazyCat Registry，或使用原镜像、显式镜像加速地址；
- 默认创建 Pull Request，也可直接提交、创建 tag 和发布；
- tag、release 或手动触发非 Docker 的静态 Web、Exec、Go、Rust、TypeScript 项目构建；
- 更新 `package.yml` 版本，执行项目自己的 `buildscript`，构建和解析 LPK；
- 对官方商店和非官方场景采用不同 lint profile；
- 创建 GitHub Release、上传 LPK，并发布到 LazyCat 官方开发者平台或喵喵私有商店；
- 在 Linux x64 和 Linux ARM64 Runner 上运行，但始终生成 LazyCat 当前要求的 Linux x86_64 目标产物。

## 2. 非目标

- 不实现通用 CI 平台；只支持 GitHub Actions。
- 不推断多服务应用中的“主服务”。版本来源和 Manifest 更新目标必须显式配置。
- 不替代 Go、Rust、Node、Docker 等项目工具链；Action 只定义并传入统一构建环境，workflow 示例负责安装工具链。
- 不在 v1 中生成或移动用户已经创建的外部 tag。
- 不支持 Windows 或 macOS Runner。
- 不支持 LazyCat ARM 应用产物。ARM64 仅是 Action 自身可运行的宿主架构。
- 不为 LazyCat 开发者平台的镜像复制接口增加其本身不支持的私有源 Registry 凭据字段。

## 3. 核心约束：运行架构与目标架构分离

Action 的运行架构和 LazyCat 应用的目标架构是两个独立概念：

| 层次 | 支持值 | 含义 |
|---|---|---|
| Runner OS | Linux | 执行 GitHub Action 的操作系统 |
| Runner 架构 | `amd64`、`arm64` | 执行 Action 二进制的宿主 CPU |
| LazyCat 目标 OS | 固定 `linux` | LPK 内程序和容器的目标 OS |
| LazyCat 目标架构 | 固定 `amd64` | LPK 内程序和镜像的目标 CPU |
| OCI 平台 | 固定 `linux/amd64` | 镜像检查、构建和复制选择的平台 |

Action 发布两个宿主二进制：

```text
lazycat-action_linux_amd64
lazycat-action_linux_arm64
```

启动脚本根据 `uname -m` 选择 Action 二进制。无论选择哪个宿主二进制，业务目标常量始终是：

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=amd64
LAZYCAT_TARGET_PLATFORM=linux/amd64
```

v1 不提供可把目标架构改成 ARM 的公开输入，防止 ARM Runner 被误解为 ARM 应用构建。相关实现必须显式指定平台：

- 调用 `appstore.CopyImage` 时设置 `CopyImageRequest.Platform: "amd64"`；
- 调用本地 Docker 镜像构造器时设置 `dockerlocal.WithPlatform("linux/amd64")`；
- 查询 OCI manifest、digest 和创建时间时只选择 `linux/amd64`；
- Docker 构建使用 Buildx 的 `--platform linux/amd64`；
- Go 示例使用 `GOOS=linux GOARCH=amd64`；
- Rust 示例使用 `x86_64-unknown-linux-gnu`；
- TypeScript 静态资源本身通常与 CPU 无关，但任何 native addon、打包运行时或 Exec 程序仍必须输出 Linux x86_64。

ARM64 Runner 进行 registry 到 registry 的远端镜像复制不需要执行 x64 镜像。本地 Docker 跨架构构建如果需要执行 Dockerfile 中的 x64 `RUN` 指令，则 Runner 必须预先配置 Buildx 和 QEMU；reusable workflow 的 Docker 示例会使用 `docker/setup-qemu-action` 和 `docker/setup-buildx-action`。

Action 输出同时包含 `runner-arch` 和 `target-platform`，日志固定打印类似：

```text
Action host: linux/arm64; LazyCat target: linux/amd64
```

## 4. 总体架构

### 4.1 两层职责

第一层是核心 Go Action：

- 读取并验证 Action 配置；
- 读取 `package.yml`、`lzc-build.yml` 和 Manifest；
- 检查 OCI 镜像版本和 digest；
- 按显式目标更新 YAML，同时尽量保留注释和字段顺序；
- 调用 `lzc-toolkit-go` 完成镜像复制、构建、LPK 解析和 lint；
- 执行项目 `buildscript`；
- 计算 SHA256；
- 发布 LazyCat 官方商店或喵喵私有商店；
- 通过 GitHub outputs 和 JSON 结果返回所有后续编排所需信息。

第二层是 reusable workflow：

- checkout 源码和 tag；
- 安装项目声明的 Go、Node、Rust 或 Docker 工具链；
- 调用核心 Action；
- 使用 `actions/upload-artifact` 上传 PR 模式构建结果；
- 使用 `peter-evans/create-pull-request` 创建或更新 PR；
- 使用 `actions/github-script` 查询和校验 Release Asset 的真实 `browser_download_url`；
- 使用 `softprops/action-gh-release` 创建 Release 和上传 LPK；
- 提交版本同步变更；
- 调用核心 Action 发布商店。

第三方 Action 在仓库文件中固定完整 commit SHA；文档为了可读性可以同时标注其主版本。

### 4.2 代码边界

Go 代码按职责拆分，公开 Action 契约与实现细节分离：

```text
cmd/lazycat-action/       进程入口和退出码
internal/action/          operation 调度、输入输出、最终 Result
internal/config/          配置模型、默认值、边界校验
internal/project/         项目类型识别、package/manifest 定位
internal/yamledit/        基于 yaml.Node 的定点编辑和注释保留
internal/registry/        OCI tag、manifest、digest、创建时间查询
internal/versioning/      channel、正则、SemVer、版本映射
internal/imageupdate/     image 配置与 Manifest 目标绑定
internal/delivery/        lazycat/direct/mirror 交付策略
internal/build/           buildscript 环境和 toolkit 构建适配
internal/lpkcheck/        LPK 解析、版本验证、lint 和 SHA256
internal/store/official/  LazyCat 官方开发者平台适配
internal/store/private/   喵喵私有商店 HTTP 客户端
internal/githubio/        GITHUB_OUTPUT、step summary、结果 JSON
internal/execx/           可测试的外部命令边界
```

这些包都是 Action 内部实现，不要求 Action 使用者导入 Go 包。LPK 业务能力复用 `lzc-toolkit-go`，不在 Action 仓库复制 SDK 逻辑。

## 5. 使用界面

### 5.1 Composite Action

`action.yml` 对外提供稳定输入：

| 输入 | 默认值 | 含义 |
|---|---|---|
| `operation` | `auto` | `auto`、`check`、`build`、`publish-official`、`publish-private` |
| `config` | `.github/lazycat-action.yml` | 项目配置路径 |
| `image-id` | 空 | 手动只检查一个显式镜像 ID |
| `version` | 空 | 手动构建版本；tag/release 时自动取得 |
| `changelog` | 空 | 发布说明；release 事件时自动取得 |
| `lpk-path` | 空 | 发布操作使用的现有 LPK 路径 |
| `download-url` | 空 | 私有商店发布所需的真实 Release Asset URL |
| `dry-run` | `false` | 只计算计划，不修改文件或远端状态 |

`operation=auto` 根据事件选择行为：

- `schedule`、`workflow_dispatch` 且未指定版本：`check`；
- `push` 到默认分支：若 `package.yml` 版本尚未发布则 `build`；
- tag 或 release：`build`；
- reusable workflow 在上传 Release Asset 后显式调用商店发布操作。

稳定输出：

| 输出 | 含义 |
|---|---|
| `changed` | 是否修改了受管文件 |
| `package-id` | LPK package ID |
| `version` | 规范化后的应用版本，不带 `v` |
| `tag` | Git tag，默认 `v<version>` |
| `lpk-path` | 构建结果绝对路径 |
| `sha256` | 64 位小写十六进制 SHA256 |
| `download-url` | 已确认的 Release Asset URL |
| `image-results` | 每个镜像的检查、复制和落点结果 JSON |
| `result-file` | 完整结果 JSON 文件路径 |
| `runner-arch` | `amd64` 或 `arm64` |
| `target-platform` | 固定 `linux/amd64` |

`image-results` 中每项使用稳定结构：

```json
{
  "id": "web",
  "target": "service",
  "service": "web",
  "platform": "linux/amd64",
  "sourceRef": "ghcr.io/acme/example:1.2.3",
  "sourceDigest": "sha256:...",
  "deliveryMode": "lazycat",
  "deliveredRef": "registry.lazycat.cloud/...",
  "copied": true,
  "copyResult": {
    "sourceImage": "ghcr.io/acme/example:1.2.3",
    "platform": "amd64",
    "lazyCatImage": "registry.lazycat.cloud/...",
    "finished": true
  }
}
```

镜像复制结果必须返回给调用者，不能只写日志。详细分层进度写入 step summary；最终引用和状态进入 outputs/JSON。

### 5.2 Reusable Workflow

仓库提供 `.github/workflows/lazycat.yml`，调用方式为：

```yaml
jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      config: .github/lazycat-action.yml
    secrets: inherit
```

workflow 接受以下显式输入：

| 输入 | 默认值 | 含义 |
|---|---|---|
| `config` | `.github/lazycat-action.yml` | Action 配置路径 |
| `operation` | `auto` | 执行操作 |
| `image-id` | 空 | 只处理一个显式镜像 ID |
| `dry-run` | `false` | 只生成计划 |
| `toolchains` | `none` | 逗号分隔的 `go`、`node`、`rust`、`docker`，支持组合 |
| `go-version` | 空 | `actions/setup-go` 使用的 Go 版本 |
| `node-version` | 空 | `actions/setup-node` 使用的 Node.js 版本 |
| `rust-toolchain` | 空 | Rust toolchain 名称或版本 |
| `node-package-manager` | `npm` | `npm`、`pnpm`、`yarn` |
| `enable-qemu` | `true` | Docker 构建时是否安装跨架构 QEMU |

reusable workflow 必须在调用核心 Action 前安装工具链，因此不能依赖核心 Action 才解析出的配置来决定 setup step。workflow 输入与项目配置中的 `build.toolchains` 同时存在时必须一致，否则失败。版本输入为空时，setup Action 使用项目的标准版本文件，如 `go.mod`、`.node-version`、`rust-toolchain.toml`；两边都没有版本声明时配置失败，不使用随时间变化的隐式稳定版本。workflow 向调用方返回 `changed`、`version`、`tag`、`lpk-path`、`sha256`、`download-url` 与 `image-results`。

## 6. 项目配置

默认配置文件为 `.github/lazycat-action.yml`。未知字段直接报错，枚举值大小写不敏感但输出统一为小写。

完整模型示例：

```yaml
version: 1

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/example.lpk

update:
  strategy: pull
  version_source:
    type: image
    image: web

build:
  toolchains:
    - kind: go
      version: "1.25.x"
    - kind: docker
  run_buildscript: true

images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/example
    channel: stable
    sort: semver
    tag_regex: '^v?\d+\.\d+\.\d+$'
    exclude_regex: 'windows|arm'
    version_regex: '^v?(?P<version>\d+\.\d+\.\d+)$'
    version_template: '{version}'
    delivery:
      mode: lazycat

stores:
  official:
    enabled: true
    create_if_missing: false
    changelog_locales: [zh, en]
  private:
    enabled: false
```

### 6.1 更新策略

`update.strategy` 支持：

- `pull`：默认值。Action 修改文件并构建验证，workflow 上传 Artifact，创建或更新 PR；不创建 tag、Release，不发布任何商店。
- `publish`：直接提交版本变更、创建 tag 和 GitHub Release、上传 LPK，并按配置发布商店。

推荐的安全流程是 schedule 创建 PR，PR 合并后由 `push` 默认分支 workflow 发布。相同 package/version 已存在时应幂等退出，不能重复创建 Release 或重复提交商店。

### 6.2 版本来源

Docker 多镜像项目必须显式指定哪个镜像驱动应用版本：

```yaml
update:
  version_source:
    type: image
    image: web
```

非 Docker tag/release 项目使用：

```yaml
update:
  version_source:
    type: git
```

不允许按 service 顺序、route 顺序或第一个镜像猜测主服务。多个镜像可一起更新，但只有一个显式 image ID 驱动 `package.yml.version`。

### 6.3 镜像目标

service 镜像必须显式绑定：

```yaml
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/example
```

application 镜像必须显式声明：

```yaml
images:
  - id: runtime
    target: application
    source: ghcr.io/acme/runtime
```

Action 不推导主服务。`target=service` 时 `service` 必填且必须在 Manifest 唯一存在；`target=application` 时禁止提供 `service`。`image-id` 只允许选择已声明 ID。

更新后的 Manifest 保留源镜像线索：

```yaml
services:
  web:
    # upstream: ghcr.io/acme/example:v1.2.3
    image: registry.lazycat.cloud/example/web:1.2.3
```

显式 `source` 优先于历史 `# upstream:` 注释。注释只用于兼容既有项目，不作为新配置的唯一真相。

## 7. Channel 与版本选择

Channel 只用于 Docker 镜像检查：

- `stable`：排除预发布版本，选择最高正式 SemVer；
- `beta`：在 alpha、beta、rc、preview 等预发布 SemVer 中选择最高版本；
- `nightly`：先按正则过滤，再按 OCI manifest 创建时间选择最新；
- `night`：`nightly` 的兼容别名，规范化后输出 `nightly`；
- `custom`：使用用户正则和显式排序策略。

`sort` 支持 `semver` 和 `created`。`stable`、`beta` 默认 `semver`，`nightly` 默认 `created`；`custom` 必须显式提供 `sort`。所有 channel 先应用 `tag_regex`，再应用 `exclude_regex`。stable/beta 未提供 `tag_regex` 时接受可解析 SemVer 的 tag；nightly/custom 必须显式提供 `tag_regex`。`version_regex` 从 tag 中提取应用版本，命名组 `version` 传给 `version_template`；stable/beta 未配置时默认去掉单个前导 `v`，`version_template` 默认 `{version}`。无法得到合法 SemVer 时直接失败，不静默使用原 tag。

nightly 的可变 tag 必须生成唯一且递增的 SemVer，例如：

```text
0.0.0-nightly.20260710153020.a1b2c3d4e5f6
```

时间来自选中 OCI manifest 的创建时间，末段来自 `linux/amd64` digest。这样同一可变 tag 指向新 digest 时仍可创建新 LPK 版本。

Registry 查询必须处理分页、OCI index、Docker manifest list、匿名 Bearer challenge、速率限制和 digest 缺失。第三方 Registry 响应在使用前严格验证。

## 8. 镜像交付

每个镜像选择一种 delivery mode：

### 8.1 `lazycat`

```yaml
delivery:
  mode: lazycat
```

Action 使用 `lzc-toolkit-go/appstore.CopyImage`，显式传 `Platform: "amd64"`，等待复制完成并将返回的 LazyCat Registry 引用写入正确的 Manifest 目标。该接口是 LazyCat 平台远端复制，不调用 Runner 本地 Docker。

源镜像若需要认证，Action 无法把 Registry 用户名/密码透传给当前 LazyCat CopyImage API；配置校验会提示使用平台可访问的镜像或其他 delivery mode。

单纯检查 Registry tag 或调用 LazyCat 平台远端复制不要求 Runner 安装 Docker。只有项目的 `lzc-build.yml` 包含需要本地构造/嵌入的 Docker 镜像，或项目自己的 buildscript 调用 Docker 时，才要求安装 Docker；ARM64 Runner 上的这类构建还需要 Buildx/QEMU 来产生 `linux/amd64` 内容。

### 8.2 `direct`

```yaml
delivery:
  mode: direct
```

Manifest 直接使用选中的源镜像引用。Action 仍按 `linux/amd64` 检查其 manifest/digest，但不复制镜像。

### 8.3 `mirror`

```yaml
delivery:
  mode: mirror
  image_template: ghcr.1ms.run/acme/example:{tag}
  require_digest_match: true
```

版本始终从原 Registry 检查，`image_template` 只决定最终运行引用。开启 `require_digest_match` 时，Action 同时解析源和镜像加速地址的 `linux/amd64` digest，不一致就失败。

### 8.4 商店约束

| 应用类型 | 官方商店 | 喵喵私有商店 |
|---|---:|---:|
| Docker + `lazycat` | 支持 | 支持 |
| Docker + `direct` | 禁止 | 支持 |
| Docker + `mirror` | 禁止 | 支持 |
| 无 Docker | 支持 | 支持 |

只要启用官方商店且存在 `direct` 或 `mirror`，配置阶段就失败。官方 profile 还检查 LazyCat Registry、图标大小不超过 200 KB、locales、SemVer 等 lzc-cli 2.0.8 的官方偏好。非官方构建只执行基础 LPK lint，不错误套用官方限制。

## 9. 无 service 和源码构建项目

静态 Web 可以只有 application route：

```yaml
application:
  routes:
    - /=file:///lzcapp/pkg/content
```

Exec 应用可以使用：

```yaml
application:
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

或：

```yaml
application:
  upstreams:
    - location: /
      backend: http://127.0.0.1:8080/
      backend_launch_command: /lzcapp/pkg/content/app
```

这些项目可以没有 `services`，也不配置 `images`。Action 不进行 Docker Registry 检查，按 Git tag 或 Release 版本更新 `package.yml.version`，再执行项目 `lzc-build.yml` 中的 `buildscript`。

项目拥有自己的构建逻辑：

```yaml
buildscript: ./scripts/build.sh
contentdir: ./dist/content
```

Action 向脚本注入：

```text
LAZYCAT_VERSION
LAZYCAT_TAG
LAZYCAT_CHANNEL
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=amd64
LAZYCAT_TARGET_PLATFORM=linux/amd64
SOURCE_DATE_EPOCH
```

脚本必须根据这些变量产生 Linux x86_64 内容。Go、Rust、TypeScript 静态和 TypeScript Exec 的文档示例会给出可直接复制的完整脚本与 workflow，不使用伪代码。

## 10. 构建与 LPK 校验

构建顺序固定为：

1. 加载配置和受管 YAML；
2. 确定目标版本；
3. 精确更新镜像引用和 `package.yml.version`；
4. 执行基础 lint；
5. 执行项目 `buildscript`；
6. 使用 `lzc-toolkit-go/build.BuildFile` 构建 LPK；
7. 使用 LPK Reader 重新打开产物；
8. 校验 package ID、版本和 Manifest 与预期一致；
9. 按是否发布官方商店选择 lint profile；
10. 流式计算 SHA256；
11. 原子移动到配置的输出路径；
12. 返回结果。

失败时不留下看似成功的最终 LPK。输出目录中的临时文件使用同目录临时名并原子替换。`dry-run` 不运行 buildscript、不复制镜像、不改文件、不提交远端，只输出选择结果和计划变更。

## 11. Tag、Release 与版本同步

外部 tag `v1.2.3` 或对应 Release 触发时：

1. 规范化为版本 `1.2.3`；
2. 在检出的 tag 内容上临时更新 `package.yml.version`；
3. 执行 buildscript；
4. 构建 LPK 并验证内部版本；
5. 计算 SHA256；
6. 创建或复用 GitHub Release；
7. 上传 LPK Release Asset；
8. 从 GitHub API 获取真实 `browser_download_url`；
9. 发布配置的商店；
10. 发布成功后将同一版本变更提交回默认分支。

版本同步提交固定为：

```text
chore: sync package version to 1.2.3 [skip ci]
```

如果 Action 自己创建 tag，则必须先在默认分支提交版本，再从该提交创建 tag。对于用户已经创建的外部 tag，Action 不移动或 force-push tag。默认分支已是目标版本时不产生空提交。

Release Asset 使用其他成熟 GitHub Action 上传；核心 Go Action只负责产物和校验。PR 模式的 LPK 上传为 Workflow Artifact，不冒充正式 Release Asset。

## 12. 商店发布

### 12.1 LazyCat 官方开发者平台

官方发布复用 `lzc-toolkit-go/appstore`：镜像复制调用 `CopyImage`，LPK 提交调用 `Publish`，从而保持与 lzc-cli 2.0.8 的实际协议一致。

认证优先级：

1. `LAZYCAT_TOKEN` GitHub Secret；
2. `LZC_CLI_TOKEN` GitHub Secret；
3. `LAZYCAT_USERNAME` 与 `LAZYCAT_PASSWORD` 登录后获得临时 token；
4. self-hosted Runner 明确配置的 token 文件。

CI 推荐使用 token，不长期保存账户密码。账户密码登录调用 `https://account.lazycat.cloud/api/login/signin`，只在内存中使用返回 token。若本地已经登录 lzc-cli 2.0.8，有效 token 的本地来源顺序是 `LZC_CLI_TOKEN`，然后是 `~/.config/lazycat/box-config.json` 的 `token` 字段；`lzc-cli config get token` 可以读取生效值，但文档会警告不要把它打印到 CI 日志。GitHub-hosted Runner 不会拥有开发机的本地登录状态。

### 12.2 喵喵私有商店

私有商店使用：

```text
APPSTORE_URL
APPSTORE_TOKEN
APP_ID（已有应用时可选配置）
```

必须先上传 GitHub Release Asset，再取得真实 `browser_download_url`。`downloadUrl` 和 `sha256` 都是必填字段，私有商店可直接信任 Action 提供的值而不重新下载计算；没有真实下载地址时禁止发布。

创建应用调用：

```http
POST /api/v1/apps
Authorization: Bearer <APPSTORE_TOKEN>
Content-Type: application/json
```

请求包含 `packageId`、`name`、`summary`、`version`、`sourceType: GITHUB`、`downloadUrl` 和 `sha256`。

已有应用上传版本调用：

```http
POST /api/v1/apps/{APP_ID}/versions
Authorization: Bearer <APPSTORE_TOKEN>
Content-Type: multipart/form-data
```

表单包含 `version`、`changelog`、`file`、`downloadUrl` 和 `sha256`。客户端验证 URL 为 HTTPS GitHub Release Asset 地址、SHA256 为 64 位小写十六进制、LPK 本地计算值与输入一致。

## 13. GitHub 权限与秘密

默认 PR workflow 需要：

```yaml
permissions:
  contents: write
  pull-requests: write
```

Release workflow 需要：

```yaml
permissions:
  contents: write
```

fork PR 不接收商店 secrets，也不执行镜像复制或发布。token、密码、Authorization、`X-User-Token`、Cookie 和带签名 URL 不得出现在日志、step summary、错误详情或结果 JSON 中。配置错误与远端错误使用稳定错误码区分，远端 401/403 不回显响应中的敏感内容。

## 14. 错误和幂等性

核心 Action 统一输出机器可读错误码和可操作的用户消息：

```text
CONFIG_INVALID
PROJECT_UNSUPPORTED
IMAGE_TARGET_NOT_FOUND
REGISTRY_AUTH_REQUIRED
VERSION_NOT_FOUND
PLATFORM_NOT_FOUND
IMAGE_COPY_FAILED
BUILD_FAILED
LPK_INVALID
OFFICIAL_LINT_FAILED
RELEASE_ASSET_MISSING
STORE_AUTH_FAILED
STORE_PUBLISH_FAILED
```

退出码非零表示当前操作失败。可重试远端错误在结果中标记 `retryable`，但 Action 本身只对 Registry GET、复制进度轮询和幂等查询进行有限指数退避，不自动重试提交、创建应用等非幂等写操作。

以下状态必须安全幂等：

- 已有相同内容 PR 时更新同一分支；
- 相同版本无变化时 `changed=false`；
- 已有 tag/Release 时校验其目标，不移动 tag；
- 已有同名同 SHA256 Asset 时复用，内容不同则失败；
- 默认分支版本已同步时不提交；
- 商店已存在相同 package/version 时先查询并按协议返回已存在结果，不创建重复版本。

## 15. 发布 Action 本身

GoReleaser 为每个语义化版本构建 Linux amd64 和 Linux arm64 二进制，生成 `checksums.txt`，并上传到该版本 GitHub Release。

`action.yml` 是 composite action，执行仓库内小型安装脚本：

1. 验证 Runner 是 Linux；
2. 把 `x86_64` 规范化为 `amd64`，把 `aarch64` 规范化为 `arm64`；
3. 根据 `action.yml` 内嵌的精确版本下载对应二进制和 checksums；
4. 校验 SHA256；
5. 从临时目录执行；
6. 清理临时文件。

发布流程更新 `v1` 浮动 tag，但二进制下载使用该提交中记录的精确版本，避免 `@v1` 与 Release Asset 版本解析不一致。发布 CI 还生成 provenance/SBOM，并验证两个架构的 `--version` 输出。

## 16. 文档和 Agent Skill

README 提供中英文版本并相互引用，包含完整上下文、概念说明、权限、secrets、Docker 是否必需和以下可直接运行示例：

- Docker stable + LazyCat Registry；
- Docker stable + mirror；
- Docker beta；
- Docker nightly；
- 默认 Pull Request；
- direct publish；
- Go Exec；
- Rust Exec；
- TypeScript 静态 Web；
- TypeScript Exec；
- tag 触发；
- release 触发；
- 官方商店；
- 私有商店创建应用；
- 私有商店上传版本；
- 非 Docker 发布；
- Workflow Artifact 与 Release Asset；
- dry-run 和手动运行；
- ARM64 Runner 执行 Action、但 Docker/Go/Rust/Node 目标固定为 Linux x86_64。

仓库同时包含：

```text
skills/lazycat-github-action/
├── SKILL.md
├── references/
├── assets/
└── evals/evals.json
```

Skill 帮助 Agent 判断 Docker、静态、Exec 和源码编译项目，生成配置与 workflow，显式绑定 service，配置 channel、delivery、更新策略、官方/私有商店，检查 permissions/secrets，并排查常见错误。Skill 自带真实输入和预期结果 eval，不只提供说明文本。

## 17. 测试策略

### 17.1 单元测试

- YAML 配置默认值、未知字段和互斥字段；
- stable/beta/nightly/custom 选择矩阵；
- tag/version 正则和 SemVer 映射；
- OCI index 中只选择 `linux/amd64`；
- service/application 精确绑定和不存在/重复错误；
- yaml.Node 编辑保留上游注释和无关字段；
- delivery mode 与商店约束；
- 镜像复制结果序列化；
- tag/release 版本同步；
- LPK 内 package/version/manifest 验证；
- 私有商店请求格式、URL 和 SHA256 校验；
- secrets 脱敏；
- amd64/arm64 宿主规范化，但目标平台永远是 `linux/amd64`。

### 17.2 集成测试

使用 `httptest` 模拟 OCI Registry、LazyCat 开发者平台和私有商店；使用临时 Git 仓库模拟 PR 前后的文件变化；使用 `lzc-toolkit-go` 构建并重新读取真实 LPK。

从 `lazycat-contrib` 组织选取公开项目作为只读 fixture 来源，固定到 commit SHA 后保存最小必要 fixture，覆盖多 service、application image、静态 Web 和 Exec。测试不依赖这些仓库主分支的实时状态。

### 17.3 端到端测试

GitHub CI 矩阵：

```text
ubuntu-latest / amd64
self-hosted Linux / arm64（可用时）
```

ARM64 缺少 self-hosted Runner 时，至少通过交叉构建、二进制 smoke test 环境和架构选择脚本测试覆盖；正式 `v1` 发布前必须在真实 Linux ARM64 上完成一次 Action smoke test。两个 Runner 的结果都必须报告 `target-platform=linux/amd64`。

## 18. 实施里程碑

### Milestone 1：Action 基础与项目构建

- 配置、Action 输入输出、Runner/目标架构分离；
- package/manifest 定点编辑；
- buildscript、LPK 构建、解析、基础/官方 lint、SHA256；
- GoReleaser 双架构发布骨架；
- tag/release 非 Docker 项目。

### Milestone 2：镜像更新与 PR/Release

- OCI channel 和 digest；
- service/application 显式绑定；
- lazycat/direct/mirror；
- ARM Runner 下固定复制 amd64；
- reusable workflow、Artifact、PR、tag 和 Release Asset。

### Milestone 3：商店、示例和 Skill

- 官方开发者平台认证与发布；
- 喵喵私有商店；
- 全部中英文示例；
- Agent Skill 和 eval；
- 真实项目兼容验证和 v1 发布流程。

每个 milestone 在功能、测试和文档完成后提交，并分别合并到 `main`；第三个 milestone 合并后，`main` 必须达到可发布 v1 的状态。

## 19. 验收标准

满足以下条件才视为 v1 完成：

- 用户只写 `uses: ca-x/lazycat-github-action@v1` 即可运行，无需编译 Action；
- Linux amd64 和 Linux arm64 都能启动对应 Action 二进制；
- 两种 Runner 上所有镜像选择、复制和构建目标均为 Linux amd64；
- Docker 多服务项目不会更新错误 service；
- 镜像复制结果可从 API outputs/JSON 读取；
- stable、beta、nightly、custom 的测试结果确定且可复现；
- `pull` 是默认策略，PR 模式不会发布商店；
- direct/mirror 无法误发官方商店；
- 无 service 的静态和 Exec 项目可由 tag/release 构建；
- `package.yml.version`、LPK 内版本、Git tag 和商店版本一致；
- Release Asset 的真实 URL 与本地 SHA256 一起传给私有商店；
- 官方认证兼容 token、账号密码和明确的本地 token 文件；
- README 中所有示例完整，且明确区分 Runner ARM64 与目标 x86_64；
- 单元测试、集成测试、Action smoke test、`go test ./...`、`go vet ./...` 和发布校验全部通过。
