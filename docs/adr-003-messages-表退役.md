# ADR-003：messages 表退役，转录 JSONL 是消息唯一事实源

日期：2026-08-13 ｜ 状态：已采纳

## 背景

项目早期消息落数据库 `messages` 表。后来对话持久化改为**线级转录**：WireTap 把每条
JSON-RPC 原样追加进 `<dataDir>/transcripts/<sessionId>.jsonl`，读路径由重建器
（`service/rebuild.go`）从转录还原成 `model.Message` 视图，消息不再写库。

改造完成后残留了三处「表还在但无人写」的认知负担：AutoMigrate 仍建表、
`Session.Messages` 仍挂级联、新读者会误以为消息在库里。

## 决定

1. `messages` 表从 AutoMigrate 移除，不再创建；旧库中已存在的表**不删除**（无害，
   避免任何数据销毁操作）。
2. `Session.Messages` 关联字段删除。
3. `model.Message` 类型**保留**：它是重建器的输出形状与 API 响应契约
   （`GET /api/sessions/{id}/messages`），前端 `types/acp.ts` 的 `Message` 与之对齐。

## 后果

- 新部署的库不再有空的 messages 表；模型与现实一致。
- 会话删除路径不变：数据库删 Session 行，转录文件由 `ChatService.Destroy` 一并清理。
- 若将来需要消息级检索，应基于转录建索引（或引入检索库），而不是复活本表——
  双写两个事实源正是本次退役要消除的问题。

## 关联

- [adr-001](adr-001-codex-claude-差异收敛.md)（统一词汇与 adapter 边界）
- README「数据模型」与「对话是怎么流起来的」小节
