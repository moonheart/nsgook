<p align="center">
  <img src="assets/Icon.png" alt="NSGook 图标" width="96" />
</p>

<h1 align="center">NSGook</h1>

<p align="center">
  基于 <a href="https://github.com/nsgo-dev/nsgo">NSGo</a> 构建的纯 Go Windows <a href="https://github.com/QL-Win/QuickLook">QuickLook</a> 快速文件预览工具。
</p>

<p align="center">
  <a href="README.md">English</a> | 简体中文
</p>

---

## 核心特性

- **空格一键快览**：选中文件轻按 `Space` 打开/关闭预览；按住 `Space`（`>= 750ms`）可松手自动关闭（由配置项 `auto_close_holding` 控制）。
- **Windows 原生集成**：内置低级全局键盘钩子与前景窗口监听，支持 Windows 文件资源管理器、桌面、Everything 及标准打开/保存文件对话框（`#32770`）；同时支持命令行启动（`--top`、`--pin`）和拖放预览。
- **多窗口固定与外观定制**：支持多窗口 Pin 固定同屏对比、窗口置顶、九宫格自适应居中缩放、Acrylic / Mica / Tabbed 半透明材质以及浅色 / 深色 / 跟随系统主题。
- **双语界面与可搜索设置**：内置简体中文与英文界面、系统托盘菜单与完整 YAML 配置持久化（支持扩展名白名单/黑名单、文本/图片/音视频/字体选项等）。
- **低内存占用设计**：隐藏复用预览窗口以避免频繁重建图形上下文，同时在卸载或切换文件时通过两阶段释放机制及时清理图片像素缓存与解码数据。

## 快捷键说明

| 按键 | 功能 |
| --- | --- |
| `Space`（轻按） | 打开或关闭当前预览 |
| `Space`（长按松开） | 临时预览并在松开后关闭（需开启 `auto_close_holding`） |
| `↑` / `↓` / `←` / `→` | 预览打开期间跟随当前选中文件自动切换 |
| `Enter` | 使用系统默认程序打开并关闭预览 |
| `Esc` | 关闭当前预览 |
| `F5` | 重新加载当前预览 |
| `F11` | 切换全屏 |

## 内置 26 个预览插件

### 核心预览器

| 插件名称 | ID | 功能说明 |
| --- | --- | --- |
| ImageViewer | `imageviewer` | 支持常见光栅与矢量图片（`PNG`、`JPEG`、`GIF`、`WebP`、`BMP`、`TIFF`、`QOI`、`ICO`、`TGA`、`Netpbm`、`SVG`），支持缩放、平移、旋转、翻转、透明棋盘格与 EXIF 元信息 |
| MarkdownViewer | `markdownviewer` | 渲染 Markdown，支持目录大纲侧栏、代码高亮、公式/图表与源码切换 |
| CsvViewer | `csvviewer` | 自动识别分隔符的 CSV/TSV 表格预览 |
| HtmlViewer | `htmlviewer` | HTML / MHTML / URL 快捷方式预览及源码切换 |
| PdfViewer | `pdfviewer` | 本地 PDF 文档检测与预览 |
| VideoViewer | `videoviewer` | 音视频播放、媒体元数据查看、封面展示及同目录 `.lrc` 歌词同步 |
| ArchiveViewer | `archiveviewer` | 浏览 `.zip`、`.tar.gz`、`.asar`、`.pak` 压缩包并支持包内文件即时预览 |
| FontViewer | `fontviewer` | TTF/OTF/WOFF 字体示例句与字形表预览 |
| TextViewer | `textviewer` | 纯文本/源码/补丁 Diff 查看，支持编码探测、行号、折行、搜索及 JSON/XML/PLIST 格式化 |
| BinaryViewer | `binaryviewer` | 十六进制查看与二进制结构解析 |

### 开发者与 Windows 专属预览器（`devviewers`）

- `PEViewer`（`peviewer`）、`ELFViewer`（`elfviewer`）、`CertViewer`（`certviewer`）、`MailViewer`（`mailviewer`）
- `AppViewer`（`appviewer`）、`OfficeViewer`（`officeviewer`，支持 DOCX / XLSX / PPTX 排版预览）、`DbViewer`（`dbviewer`，SQLite 数据库）
- `Model3DViewer`（`model3dviewer`）、`CLSIDViewer`（`clsidviewer`）、`PrefetchViewer`（`prefetchviewer`）
- `DumpViewer`（`dumpviewer`）、`ChmViewer`（`chmviewer`）、`PluginInstaller`（`plugininstaller`）
- `ThumbnailViewer`（`thumbnailviewer`）、`MediaInfoViewer`（`mediainfo`）

### 兜底面板

- `InfoPanel`（`infopanel`）—— 展示文件/目录/磁盘基础属性、Shell 缩略图与哈希校验计算。

## 开发与构建

### 环境要求

- **Go 版本**：与 `go.mod` 一致（`go 1.27.3+`）
- **操作系统**：运行与打包原生桌面端需 Windows x64（无头测试可通过 `go tool nsgo test` 执行）

### 常用命令

```sh
# 静态响应式规则检查与 vet
go tool nsgo check

# 运行全部无头单元测试与内存回归测试
go tool nsgo test

# 开发模式运行
go tool nsgo dev

# 构建 Windows x64 可执行文件（产物输出至 dist/）
go tool nsgo build -target windows_x64
```

### 发布版本

推送 `v*` 标签即可自动构建并发布 GitHub Release：

```sh
git tag v0.1.0
git push origin v0.1.0
```

Release 工作流会依次执行 `nsgo check`、`nsgo test`，构建 Windows 安装包，根据 Conventional Commits 生成发布说明，创建 Release，并把更新后的 [CHANGELOG.md](CHANGELOG.md) 提交回 `main`。带 `-` 的标签（如 `v0.1.0-rc.1`）会标记为预发布。

### 命令行参数

```sh
NSGook.exe [--top] [--pin] <文件路径>...
```

- `--top` / `-top` / `/top`：以窗口置顶方式打开预览
- `--pin` / `-pin` / `/pin`：以固定多窗方式打开预览

## 目录结构

```text
nsgook/
├── main.go              # 程序入口与命令行参数解析
├── nsgo.yaml            # NSGo 应用配置与打包清单
├── assets/              # 内嵌应用图标资源
├── locales/             # 英文（en）与简体中文（zh-Hans）多语言目录
├── app_test.go          # 端到端无头 UI 与内存边界测试
└── internal/
    ├── app/             # 控制器、ViewerSession、设置窗口与九宫格布局定位
    ├── config/          # YAML 配置读写与扩展名过滤
    ├── sys/             # Win32 全局键盘钩子、资源管理器选区读取与文件监听
    ├── viewer/          # 插件接口定义、注册表与 Context 生命周期
    └── plugins/         # 26 个内置预览插件及共享 UI 组件（pluginui）
```
