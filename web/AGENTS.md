# 前端规范（web/）

适用于 `web/` 下所有前端代码，含设计规范。通用规则（分包原则、跨端契约、提交与验证）见 [../AGENTS.md](../AGENTS.md)，此处只讲前端。

## 1. 目录职责

| 目录 | 职责 | 禁令 |
| --- | --- | --- |
| `src/routes/` | 页面，与路由表（`App.tsx`）一一对应 | 不放可复用组件 |
| `src/components/` | 跨页面复用的组件；同一功能域 ≥3 个时建子目录 | 不放页面 |
| `src/components/ui/` | shadcn CLI 生成 | **禁止手改**，升级会整体覆盖。需要变体就在外面包一层 |
| `src/hooks/` | 自定义 hooks | 不放纯函数（那是 `lib/`） |
| `src/lib/` | 纯函数与客户端（`api.ts`、`utils.ts`） | 不出现 JSX、不依赖组件 |
| `src/types/` | 领域类型，`acp.ts` 与 `server/internal/model` 字段对齐 | 组件 props 类型不放这里，跟组件走 |
| `src/i18n/` | 语言配置与资源 | — |

依赖方向：`routes → components → ui`；`hooks / lib / types` 被上层引用，不反向 import 组件。路径别名统一 `@/`。

反平铺举例：聊天相关组件 `tool-call.tsx`、`markdown.tsx` 再加第三个时，应建 `components/chat/` 并把三者移入。`components/` 目前的平铺是历史包袱——新文件按新规则放，旧文件顺路触碰时迁移。

## 2. 命名

| 场景 | 规则 | 示例 |
| --- | --- | --- |
| 文件 | 全部 kebab-case | `session-chat.tsx`、`use-chat.ts` |
| React 组件 | PascalCase 具名导出，与文件名对应 | `app-sidebar.tsx` 导出 `AppSidebar` |
| 自定义 hook | 文件 `use-xxx.ts`，函数 `useXxx` | `use-chat.ts` → `useChat` |
| 类型/接口 | PascalCase，领域类型集中在 `types/acp.ts` | `Session`、`SessionCaps` |
| 常量 | 模块级不可变配置用 UPPER_SNAKE，其余 camelCase | `BASE` |
| i18n key | 点分层级 `域.用途` | `chat.send`、`agents.title` |

## 3. 文件拆分

- 页面或组件超过 ~300 行是拆分信号：可命名的子组件、纯逻辑抽出去。
- 子组件只被一个页面用 → 放该功能域的组件子目录；纯逻辑 → `lib/` 或自定义 hook。
- 拆出的文件必须有独立可命名的职责，`session-chat-part2.tsx` 这种碎片不如不拆。

## 4. 硬规则

- **所有用户可见文案必须走 `t()`**，key 同时加进 `i18n/locales/zh.ts` 和 `en.ts`（zh 是兜底；`i18next.d.ts` 的类型增强会在编译期抓漏写的 key）。JSX 里出现中文/英文字面量即违规。
- **后端请求只走 `lib/api.ts`**，组件内禁止裸 `fetch`。新端点：先在 `api.ts` 加方法、`types/acp.ts` 补类型，再在组件用。
- 格式化交给 Prettier（`npm run format`），不手动调格式、不在 code review 里争格式。
- 若确需把新的生成文件加入 lint 豁免，同步维护 `eslint.config.js` 里的豁免清单。
- 状态管理：优先组件局部 state 与自定义 hook（参考 `use-chat.ts`）。**不引入**全局状态库（redux/zustand 等），确有跨页面共享需求先在任务里讨论。
- SSE / 流式逻辑集中在 `hooks/use-chat.ts` 的状态机里，组件只消费状态，不直接操作 EventSource。

## 5. 设计规范（UI/UX）

视觉基调：macOS 原生 app 质感（目标是打包为桌面应用）。系统字体栈（macOS 上即 SF Pro）、箭头光标（不用手型）、chrome 区不可选中、overlay 细滚动条、按压 scale 反馈、强 easing（`ease-snappy` 等，定义在 `index.css`）、克制的毛玻璃材质。布局骨架沿用 shadcn dashboard-01。完整主题方案通过 `<html data-palette>` 切换（见 `lib/palette.ts` 与 `index.css` 的 data-palette 块）：每套定义全量语义 token（背景/表面/边框/侧栏/主色），light 与 dark 各一版，与明暗切换正交。新增主题 = index.css 加一个 data-palette 块（两版）+ `lib/palette.ts` 注册；改动语义 token 时必须核对五套方案 × 明暗两模式都成立。

- **组件**：优先用现有 `components/ui/` 的组件；缺什么用 `npx shadcn@latest add <component>` 添加。组件基于 **Base UI**（不是 Radix）：自定义触发元素用 `render={<Link to="..." />}`，**没有 `asChild`**。
- **颜色**：只用 `index.css` 定义的语义 token（`bg-background`、`text-muted-foreground`、`border-border`、`text-destructive` 等）。**禁止**硬编码色值（`#hex`、`rgb()`、`text-blue-500` 这类原始色板类）。需要新颜色 → 先在 `index.css` 定义语义变量。
- **暗色模式**：语义 token 天然双主题。任何新界面必须在 light / dark 下都检查过。
- **图标**：只用 `lucide-react`，默认 `size-4`，与文字并排时对齐基线。
- **间距/圆角**：用 Tailwind 标度（`gap-2` / `p-4` / `rounded-lg`），不写任意值（`p-[13px]`）除非对齐第三方像素。
- **类名合并**：一律 `cn()`（`lib/utils.ts`），不手拼字符串。
- **三态完整**：每个数据页面必须处理 loading / error / empty 三态，不允许白屏或裸报错。错误提示与 toast 用 `sonner`。
- **响应式**：移动端判断用 `use-mobile.ts`，布局断点用 Tailwind `md:` 等前缀。侧边栏等既有响应式行为不破坏。
- **可访问性**：可点击元素用 `<button>` / `<a>`（或 shadcn 等价物），不给 `div` 挂 onClick；表单控件有 label；纯图标按钮加 `aria-label`（文案同样走 i18n）。
