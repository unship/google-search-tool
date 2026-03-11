---
name: google-search
description: "做计划、调研、选型、遇困难或知识盲区时用 google-search-tool 先搜再答，避免只靠记忆或本地。"
---

# Google Search

本地 CLI：`google-search-tool`，输入检索词执行 Google 搜索, 获取连接标题, 连接地址, 摘要。


## 何时用（先搜再答）

- **做计划**：拆任务、定步骤前，先搜相关实践/踩坑。
- **调研 / 选型**：问「用什么实现 X」「有没有做 Y 的包/插件」「A 和 B 哪个好」时，先搜索再给方案。
- **遇困难**：报错、卡住、不确定实现方式时，先搜再修。
- **知识盲区**：不熟的技术、新版本、最新文档/新闻时，先搜再答。

## 命令

```bash
google-search-tool search "query"   # 必用
# -n N 条数(默认100)  --time day|week|month|year  --lang zh-CN|en
```

示例：`google-search-tool search "汕头 烟花晚会 2025" -n 20`

