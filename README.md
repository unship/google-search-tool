# google-search-tool


## 依赖

- [agent-browser](https://github.com/agent-browser/agent-browser)（浏览器自动化 CLI）

## 子命令（3 个）

| 子命令 | 说明 |
|--------|------|
| **mcp** | 启动 MCP 服务（stdio），向 agent 暴露 `search`、`fetch` 两个 tool |
| **search** | 使用 agent-browser 执行 Google 搜索，输出标题与链接 |
| **fetch** | 抓取 URL 正文并转为 **Markdown**，自动清理广告与无关块 |

## 使用方式

```bash
# 构建（推荐 CGO_ENABLED=0）
make build
# 或
CGO_ENABLED=0 go build -o google-search-tool .

# 1. 搜索（原 -q/-n 等改为子命令 search）
./google-search-tool search -q "python tutorial" -n 5
./google-search-tool search "python tutorial" -n 5
./google-search-tool search -q "python" --cdp 9222
./google-search-tool search -q "python" --cdp 0 --llm

# 2. 抓取 URL 正文（输出 Markdown，并清理广告/脚本/导航等）
./google-search-tool fetch https://example.com

# 3. 启动 MCP 服务（供 Claude Desktop / 其他 MCP 客户端连接）
./google-search-tool mcp
./google-search-tool mcp --cdp 0   # search 时自动起新浏览器
```

**search 参数：**

- `-q` / `--query`：搜索关键词（必填，也可用位置参数）
- `-n` / `--num`：返回前 N 条，默认 100（不足时自动翻页）
- `--cdp`：CDP 端口，默认 9222；传 `0` 则自动起新浏览器
- `--llm`：输出为 LLM 友好格式

**mcp 参数：**

- `--cdp`：内部调用 search 时使用的 CDP 端口（默认 9222）

## 输出示例

```
Python Tutorial - W3Schools
https://www.w3schools.com/python/
---
Official Python Tutorial
https://docs.python.org/3/tutorial/
---
...
```

## 说明

- 工具通过 agent-browser 打开 Google 搜索，每页等待约 2 秒后执行页面内 JS 提取结果；若当前已获取条数不足 N，会自动请求下一页（`&start=10,20,...`），按 URL 去重后输出前 N 条。
- 带 `#:~:text=` 的「Read more」式链接会被过滤，不纳入结果。
- 若 Google 返回验证页（如 CAPTCHA），则可能无法得到预期条数，需在浏览器中完成验证或使用代理/合适网络环境。
- 需要本机已安装 agent-browser 且可访问 Google。
- 默认连接本机 CDP 9222，请先用 `--remote-debugging-port=9222` 启动 Chrome。若需自动起新浏览器，传 `--cdp 0`。

### Result processing（参考 duckduckgo-mcp-server）

- **Removes ads and irrelevant content**：过滤广告/赞助类标题及已知无关域名（如 doubleclick、googleadservices、google.com/sorry 等）。
- **Cleans up redirect URLs**：清洗 Google 重定向链接（`google.com/url?url=...`）为最终目标 URL。
- **Formats for LLM**：使用 `-llm` 时输出为「Found N results」+ 编号 + Title/URL 的格式，便于大模型直接消费。
- **Truncates long content**：标题超过 300 字符会自动截断并加 `...`。

### Error handling

- **Comprehensive error catching and reporting**：所有外部调用（打开页面、执行提取、JSON 解析）均有明确错误信息，统一用 `[INFO]` / `[ERROR]` 打到 stderr。
- **Graceful degradation**：打开页面或执行提取失败时会自动重试（默认各 2 次，间隔 3 秒），减轻偶发超时或限流影响；某一页解析失败但已收集到部分结果时，会跳过该页继续翻页而非直接退出。
- **Exit codes**：`1` 用法错误，`2` 运行时错误（浏览器/打开/解析等），`3` 未解析到任何搜索结果。

### 若出现 segmentation fault

1. **先确认是否为 Go 运行时问题**  
   ```bash
   make test-minimal && ./minimal
   ```  
   若 `./minimal` 也崩溃，说明是 **Go 开发版**（如 `go1.26-devel`）的运行时问题，请用稳定版 Go 构建主程序：

   ```bash
go install golang.org/dl/go1.22.0@latest
go1.22.0 download
make build-stable
./google-search-tool search -q cpp
```
或直接使用：`go1.22.0 build -o google-search-tool .`

2. 主程序已改为从嵌入的 `extract.js` 读取脚本，若仅主程序崩溃而 `./minimal` 正常，请把运行命令与报错发到 issue。

**安装到 PATH**：`make install` 会复制到 `$(go env GOPATH)/bin`，确保该目录在 PATH 中即可全局使用 `google-search-tool`。

---

## 将 MCP 加到 Cursor 和 OpenCode

MCP 通过 **stdio** 通信，只需配置「用哪个命令启动」即可。

### 1. 先构建并确定可执行文件路径

```bash
make build
# 可执行文件：当前目录的 ./google-search-tool，或 make install 后的 $(go env GOPATH)/bin/google-search-tool
```

记下**绝对路径**，例如：
- 项目内：`/Users/你/go/src/github.com/ai/google-search-tool/google-search-tool`
- 或 PATH 里：`/Users/你/go/bin/google-search-tool`

---

### 2. Cursor

1. 打开 **Cursor** → **Settings**（或 `Cmd + ,`）
2. 搜索 **MCP** 或进入 **Features → MCP**
3. 点击 **Edit in settings.json**（或直接打开 MCP 配置文件）

配置格式为 **JSON**，在 `mcpServers` 里增加一项，例如：

```json
{
  "mcpServers": {
    "google-search-tool": {
      "command": "/Users/你/go/bin/google-search-tool",
      "args": ["mcp"]
    }
  }
}
```

若可执行文件在项目目录下，则用完整路径：

```json
"google-search-tool": {
  "command": "/Users/你/go/src/github.com/ai/google-search-tool/google-search-tool",
  "args": ["mcp"]
}
```

保存后 Cursor 会重连 MCP；在对话里即可使用 **search**（Google 搜索）和 **fetch**（抓取网页 Markdown）两个工具。

- 使用 search 前需本机有 [agent-browser](https://github.com/agent-browser/agent-browser)，且可访问 Google（或开好 CDP 浏览器）。
- 若希望 search 时自动起新浏览器，可加参数：`"args": ["mcp", "--cdp", "0"]`。

---

### 3. OpenCode

编辑 OpenCode 的配置文件（例如 `~/.config/opencode/opencode.json` 或你实际使用的路径），在 **mcp** 里增加一个 **local** 类型的 server：

```json
{
  "mcp": {
    "ddg-search": { ... },
    "google-search-tool": {
      "type": "local",
      "command": ["/Users/你/go/bin/google-search-tool", "mcp"],
      "enabled": true
    }
  }
}
```

`command` 必须是**数组**：第一个元素为可执行文件绝对路径，第二个为子命令 `mcp`。保存后重启 OpenCode 或执行 `opencode mcp list` 检查是否出现 `google-search-tool` 且为已连接。

在 agent 的 `permission` 或 `tools` 里允许使用该 MCP 暴露的工具（例如 `google-search-tool_search`、`google-search-tool_fetch`，具体以 OpenCode 展示为准）后，即可在对话中调用。
