# LazyCat GitHub Action

[English](README.md)

`ca-x/lazycat-github-action` 用于在 GitHub Actions 中构建和校验 LazyCat LPK 应用。它使用 [`github.com/lib-x/lzc-toolkit-go`](https://github.com/lib-x/lzc-toolkit-go) `v0.1.0`，该 SDK 的兼容基线是 `@lazycatcloud/lzc-cli` `2.0.8`。

项目按里程碑交付。Milestone 1 支持以 Git tag/版本驱动的静态 Web 和 Exec 应用；Docker 镜像发现与复制、自动 Pull Request、GitHub Release 和商店发布将在后续里程碑加入。

## 最重要的架构规则

运行 Action 的机器架构和 LazyCat 应用的目标架构是两件事：

| 层次 | 支持值 |
|---|---|
| GitHub Runner 系统 | Linux |
| GitHub Runner CPU | amd64 或 arm64 |
| LazyCat 目标系统 | Linux |
| LazyCat 目标 CPU | amd64（x86_64） |
| OCI 目标平台 | `linux/amd64` |

ARM64 self-hosted Runner 会下载 ARM64 版本的 Action 二进制，但项目 buildscript 始终收到：

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=amd64
LAZYCAT_TARGET_PLATFORM=linux/amd64
```

因此 LPK 内的 Go、Rust、Node.js native addon、嵌入运行时和 Docker 镜像都必须保持 Linux x86_64。Action 会明确打印：

```text
Action host: linux/arm64; LazyCat target: linux/amd64
```

## 基本概念

- `package.yml` 保存 package ID、应用版本、显示信息和 locales。
- `lzc-manifest.yml` 描述 LazyCat application、routes、可选 application 镜像和 services。
- `lzc-build.yml` 指定内容目录和项目自己的 `buildscript`。
- LPK 是以上配置和应用内容共同构造出的安装包。
- 基础 lint 检查 LPK 是否可用；官方 lint 额外检查懒猫开发者平台偏好，例如 locales、官方镜像 Registry、图标格式/大小和 SemVer。
- Workflow Artifact 是 CI 产物；GitHub Release Asset 是带正式版本的下载资源。Milestone 1 上传 Artifact，Release Asset 自动化在 Milestone 2 实现。

## 最小项目配置

创建 `.github/lazycat-action.yml`：

```yaml
version: 1

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/app.lpk

update:
  strategy: pull
  version_source:
    type: git

build:
  run_buildscript: true
```

未知字段会直接报错。Milestone 1 要求 `version_source.type: git`；镜像驱动的版本检查在 Milestone 2 实现。

## 静态 Web 示例

静态应用可以完全没有 services：

```yaml
application:
  subdomain: example
  routes:
    - /=file:///lzcapp/pkg/content
```

`lzc-build.yml`：

```yaml
buildscript: ./scripts/build.sh
contentdir: ./dist/content
```

TypeScript 静态站点的 `scripts/build.sh` 可以是：

```bash
#!/usr/bin/env bash
set -euo pipefail
npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
cp -R web-dist/. dist/content/
```

静态 HTML/CSS/JavaScript 通常与 CPU 无关；如果应用运行时包含 Node.js native addon，它仍必须编译为 Linux x86_64。

## Exec 示例

Exec 应用也不要求 services：

```yaml
application:
  subdomain: example
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

Go buildscript 必须使用 Action 提供的目标变量，不能使用 Runner 自身架构：

```bash
#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist/content
CGO_ENABLED=0 \
GOOS="${LAZYCAT_TARGET_OS}" \
GOARCH="${LAZYCAT_TARGET_ARCH}" \
go build -trimpath -ldflags='-s -w' -o dist/content/app ./cmd/app
```

无论 Runner 是 amd64 还是 ARM64，以上脚本都输出 Linux x86_64 程序。

## Git tag Workflow

Action 发布 `v1` 后，调用者不需要编译它：

```yaml
name: Build LPK

on:
  push:
    tags:
      - "v*"

permissions:
  contents: read

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: ca-x/lazycat-github-action@v1
        id: lazycat
        with:
          operation: build
          version: ${{ github.ref_name }}

      - uses: actions/upload-artifact@v4
        with:
          name: lpk-${{ steps.lazycat.outputs.version }}
          path: ${{ steps.lazycat.outputs.lpk-path }}
```

Action 会去掉一个前导 `v`，更新 `package.yml.version`，运行项目 buildscript，构建 LPK，再次打开 LPK 校验 package ID/版本，执行 lint，计算 SHA256 并返回 outputs。

如需使用 ARM64 Runner，只修改 `runs-on` 为对应 ARM64 标签，不要修改 LazyCat 目标变量。

## Dry run

```yaml
- uses: ca-x/lazycat-github-action@v1
  with:
    operation: build
    version: v1.2.3
    dry-run: true
```

Dry run 只读取项目并报告 `package.yml` 是否需要变化；不会修改文件、执行 buildscript 或生成 LPK。

## Outputs

主要 outputs 包括 `changed`、`package-id`、`version`、`tag`、`lpk-path`、`sha256`、`result-file`、`runner-arch` 和 `target-platform`。Milestone 1 的 `image-results` 是空 JSON 数组，Milestone 2 会填入镜像检查和复制结果。

完整且不含秘密的结果写入 `.lazycat-action/result.json`。密码、平台 token、商店 token、Authorization 请求头和 Cookie 不会写入 outputs 或 step summary。

## Runner 是否需要安装 Docker

Milestone 1 的静态和 Exec 构建不需要 Docker，除非项目自己的 `buildscript` 调用 Docker。后续的 Registry 版本检查和 LazyCat 平台远端 Registry-to-Registry 镜像复制同样不要求本地 Docker。ARM64 Runner 如果要执行 x64 Dockerfile 构建步骤，则需要 Buildx/QEMU，并显式构建 `linux/amd64`。

## 当前 Milestone 1 限制

为了保持后续 API 只新增能力，`check`、`publish-official` 和 `publish-private` 输入已经存在，但在相应里程碑完成前会返回 `PROJECT_UNSUPPORTED`。Milestone 1 不创建 Pull Request、tag、Release、Release Asset 或商店提交。

完整目标行为见[设计规格](docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md)。
