---
name: google-search
description: "Search Google using google-search-tool CLI. Use when the user needs to look something up on the web"
---

# Google Search

Local CLI tool `google-search-tool` for Google search and webpage fetching. Runs via agent-browser under the hood.

**Binary:** `google-search-tool`

## When to use

- User asks to "search", "look up", "find … on the web"
- User gives a URL and wants a summary
- Need recent info (news, events, dates)

## CLI Commands

### Search

```bash
google-search-tool search "your query"
```

**Options:**
- `-n N` / `--num N` - Number of results (default: 100)
- `--time day|week|month|year` - Time filter
- `--lang zh-CN|en` - Language
- `--llm` - LLM-friendly output format
- `--json` - JSON output

**Example:**
```bash
google-search-tool search "汕头 烟花晚会 2025" -n 5 --llm
```
