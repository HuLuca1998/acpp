# ui/ 目录规范（shadcn 托管区）

本目录全部文件由 shadcn CLI 生成为起点，**允许直改**打磨细节（动效、质感），代码归我们所有。
设计系统的完整规则在 [web/AGENTS.md](../../../AGENTS.md) §5，此处只有目录级约束：

- **升级组件必须 `npx shadcn@latest add <c> --diff` 对比后手动合并**，禁止盲目 `--overwrite` 冲掉本地打磨。
- 定制的优先级：语义 token → `index.css` 视觉深度层（`[data-slot]`）→ 直改本目录组件 → 外层包装。能在上游解决的不下沉到这里。
- 本目录豁免结构检查的目录数与行数软线（文件数由 CLI 决定，不可分包）；行数硬线（800）仍适用。
- 新增生成文件若需 lint 豁免，维护在 `web/eslint.config.js` 的豁免清单，不要在文件内散落 disable 注释。
- 基于 **Base UI**（不是 Radix）：自定义触发元素用 `render={...}`，**没有 `asChild`**。
