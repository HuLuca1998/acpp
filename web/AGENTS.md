# 前端规范（web/）

适用于 `web/` 下所有前端代码，含设计规范。通用规则（分包原则、跨端契约、提交与验证）见 [../AGENTS.md](../AGENTS.md)，此处只讲前端。

## 1. 目录职责

| 目录 | 职责 | 禁令 |
| --- | --- | --- |
| `src/routes/` | 页面，与路由表（`App.tsx`）一一对应 | 不放可复用组件 |
| `src/components/` | 跨页面复用的组件；同一功能域 ≥3 个时建子目录 | 不放页面 |
| `src/components/shell/` | 应用外壳：侧边栏、顶栏、导航、主题/语言切换 | — |
| `src/components/chat/` | 聊天消息渲染；`composer/` 收输入域、`cards/` 收交互卡片 | — |
| `src/components/workspace/` | 会话工作区编排（dock/menu/provider）；面板全在 `panels/` | — |
| `src/components/overview/` | 概览页专用卡片 | — |
| `src/components/settings/` | 设置页分区面板（内置工具 claude/codex 的配置面） | — |
| `src/components/ui/` | shadcn 组件（CLI 托管，目录级规范见其 AGENTS.md） | 升级用 `--diff` 手动合并，禁止盲目 `--overwrite` |
| `src/hooks/` | 自定义 hooks | 不放纯函数（那是 `lib/`） |
| `src/lib/` | 纯函数与客户端；**索引在 [src/lib/README.md](src/lib/README.md)** | 不出现 JSX、不依赖组件 |
| `src/types/` | 领域类型，`acp.ts` 与 `server/internal/model` 字段对齐 | 组件 props 类型不放这里，跟组件走 |
| `src/i18n/` | 语言配置与资源 | — |

依赖方向：`routes → components → ui`；`hooks / lib / types` 被上层引用，不反向 import 组件。路径别名统一 `@/`。`components/` 根只留真正跨域的小组件（status-dot / diff-view / dir-picker / agent-icon / list-page-states），新组件优先归入功能域子目录。

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
- **后端请求只走 `lib/api.ts`**，组件内禁止裸 `fetch`（`make check-structure` 强制）。新端点：先在 `api.ts` 加方法、`types/acp.ts` 补类型，再在组件用。
- **写逻辑前先查工具索引 [src/lib/README.md](src/lib/README.md)**：同类工具已有就复用；≥2 处需要的逻辑进 `lib/`（纯函数）或 `hooks/`（React 逻辑），并同步索引（脚本对账）。列表页三态用 `ListPageStates`，一次性加载用 `useAsyncData`，状态色调用 `lib/status-tone.ts`。
- 格式化交给 Prettier（`npm run format`），不手动调格式、不在 code review 里争格式。
- 若确需把新的生成文件加入 lint 豁免，同步维护 `eslint.config.js` 里的豁免清单。
- 状态管理：优先组件局部 state 与自定义 hook（参考 `use-chat.ts`）。**不引入**全局状态库（redux/zustand 等），确有跨页面共享需求先在任务里讨论。
- SSE / 流式逻辑集中在 `hooks/use-chat.ts` 的状态机里，组件只消费状态，不直接操作 EventSource。

## 5. 设计系统规范（UI/UX）

### 5.0 视觉基调

macOS 原生 app 质感（目标是打包为桌面应用）：系统字体栈（macOS 上即 SF Pro）、箭头光标（不用手型）、chrome 区不可选中、overlay 细滚动条、按压 scale 反馈、强 easing、克制的毛玻璃材质。布局骨架沿用 shadcn dashboard-01。整体气质是**安静的专业工具**：颜色只出现在需要被看见的地方（状态点、主按钮、危险操作），大面积永远是中性的纸面色。

### 5.1 Token 体系

一切视觉常量都是 `index.css` 里的语义 token，组件里**禁止**硬编码色值（`#hex`、`rgb()`、`text-blue-500` 等原始色板类）。需要新颜色 → 先在 `index.css` 定义语义变量，再用。

- **界面色**：`bg-background` / `bg-card` / `bg-popover` / `bg-muted` / `bg-accent`、`text-foreground` / `text-muted-foreground`、`border-border`、`ring-ring`。
- **意图色**：`primary`（品牌与主操作）、`destructive`（危险）、`success`（活跃/健康）、`warning`（注意）。success/warning 跨全部 palette 恒定，只在 `:root` / `.dark` 定义。
- **主题方案**：`<html data-palette>` 切换（`lib/palette.ts` + `index.css` 的 data-palette 块），每套全量定义语义 token，light/dark 各一版，与明暗切换正交。新增主题 = index.css 加一个 data-palette 块（两版）+ `palette.ts` 注册。改语义 token 必须核对**五套方案 × 明暗两模式**都成立。
- **圆角**：`rounded-sm…4xl` 全部由 `--radius` 推导，不写任意值。间距用 Tailwind 标度（`gap-2` / `p-4`），不写 `p-[13px]` 除非对齐第三方像素。
- **字体**：正文系统栈；命令、路径、id、版本号一律 `font-mono`；数字列表列加 `tabular-nums` 防跳动。
- **材质与光感（视觉深度层）**：`index.css` 末段的无层级 data-slot 块统一负责——内容区主色氛围光晕、卡片投影与深色受光顶缘、实心按钮键帽高光、侧栏当前项主色浸染。这些全部由 `--primary` 等语义 token 推导，**新 palette 零成本继承**；不要在单个组件里手写 box-shadow/渐变去仿造同类效果，往这个层里加。

### 5.2 组件规则（shadcn 之上）

- **先查 shadcn 有没有，再决定写不写**。这是硬规则，不是建议：动手写任何 UI 之前，对着 [shadcn 组件清单](https://ui.shadcn.com/docs/components) 找一遍，有对应的就 `npx shadcn@latest add <component>` 装来用；确认没有，才自己写。手写一个「差不多的」下拉建议框、滚动区、快捷键徽标，看起来省事，实际是把别人打磨过的键盘可达性、无障碍属性、暗色适配全部重做一遍——而且多半做得更差。
  - 装组件时 CLI 会问要不要覆盖已存在的文件：**一律回答 no**，再按下一条的 `--diff` 流程手动合并。
  - 已装组件见 `components/ui/`；常被漏掉的有 `combobox`（带建议的输入）、`command`（命令面板）、`item`（列表行）、`kbd`（快捷键徽标）、`scroll-area`（滚动区）、`attachment`（附件芯片）、`questionnaire`（一次一问的表单流，AI 追问卡用它）、`data-table`（TanStack Table，排序/列显隐/分页）。
  - **查过之后判断「不合适」也是合格的结论**，但要把理由写在代码里，别让下一个人再查一遍。已有的判例：输入框的斜杠补全菜单不用 `command`——cmdk 的 Root 无条件吞掉方向键与 Enter，而 composer 是多行 textarea，套上去光标就动不了了（见 `chat/composer/composer.tsx`）。
- **优先组合现有 `components/ui/`**。基于 **Base UI**（不是 Radix）：自定义触发元素用 `render={<Link to="..." />}`，**没有 `asChild`**。
- **`ui/` 以 shadcn 生成为起点，允许直接修改**——组件代码归我们所有，动效、细节不合基调就直接改（Dialog 的入出场就是这么调的）。约束两条：
  1. 升级组件必须 `npx shadcn add <c> --diff` 对比后**手动合并**，禁止盲目 `--overwrite` 冲掉本地打磨；
  2. 定制的**优先级**依然是：语义 token（改一处全局生效）→ `index.css` 视觉深度层（`[data-slot="…"]` 一次覆盖所有实例）→ 直改单个 ui/ 组件（只影响该组件）→ 外层包装（新增语义时）。能在上游解决的不下沉。
- **专用组件优先于裸标签**：空态用 `Empty`，加载用 `Skeleton`（形状贴近真实内容），提示条用 `Alert`，危险确认用 `AlertDialog`（**禁用原生 `window.confirm`**），toast 用 `sonner`，分隔用 `Separator`，标签用 `Badge`。表单用 `FieldGroup`/`Field`，按钮内图标用 `data-icon="inline-start|end"`。
- **聊天界面**只用 chat 原语：`MessageScroller`（滚动/跟随/回到底部）、`Message`、`Bubble`、`Marker`，不手写滚动容器与气泡 div。消息种类的专用渲染集中在 `components/chat/`：markdown 正文（代码块带语言标签 + 复制）、工具调用（按 ACP kind 换图标，diff/终端视图）、任务计划 `PlanCard`、思考折叠 `ThoughtBlock`、复制按钮。新消息种类先在这里建组件，不在页面里内联。
- **图标**：只用 `lucide-react`，默认 `size-4`，与文字并排对齐基线；纯图标按钮必须有 `aria-label`。
- **类名合并**一律 `cn()`；布局用 `flex gap-*`，不用 `space-x/y-*`；等宽高用 `size-*`。

### 5.3 状态语言

运行状态统一用 `StatusDot`（`components/status-dot.tsx`），不用填充式 Badge 表达状态——颜色只落在 6px 的点上：

| tone | 含义 | 用例 |
| --- | --- | --- |
| `success` | 活着/健康/进行中 | agent connected、会话 running、后端 ok |
| `muted` | 静止/中性 | idle、ended、disabled |
| `destructive` | 出错 | agent error、会话 error、后端不可达 |
| `warning` | 需要注意（预留） | 降级、重连中 |

优先级：**出错 > 运行中 > 其他**。「正在运行」加 `pulse`（呼吸动画），静止状态永远不动。

### 5.4 动效系统

写动效前先过三问：**多久看一次？为了什么？多快结束？**

- **频率决定有无**：高频动作（键盘触发、每日百次）不加动画；中频（hover、导航）只做 ≤150ms 的颜色/透明度过渡；低频（弹窗、入场、空态）可以完整动画；等待场景（thinking、加载）允许持续性动效（shimmer/breathe）。
- **easing 只用三条曲线**（`index.css` 定义）：`ease-snappy`（默认，进出场与交互反馈）、`ease-fluid`（屏上移动/形变）、`ease-drawer`（抽屉/sheet）。**禁止 ease-in 开头的 UI 动画**。
- **时长上限**：按压反馈 100–160ms；tooltip/popover 125–200ms；下拉/展开 150–250ms；弹窗/抽屉 200–300ms。**UI 动画不超过 300ms**。
- **入场规范**：用 `@starting-style`（Tailwind `starting:` 变体），从 `opacity-0 + 位移 ≤8px`（或 `scale ≥0.95`）进入，**永远不从 `scale(0)` 开始**；列表/卡片群入场用 30–80ms stagger（参考 overview 指标卡）。
- **只动 `transform` 与 `opacity`**（外加 clip-path），不动 width/height/padding。可被连续触发的用 CSS transition（可中断重定向），不用 keyframes。
- **按压反馈**是全局的（`index.css` 的 data-slot 层，`:active` scale 0.97），新组件挂上对应 `data-slot` 即自动获得，不要在组件里重复写。
- **持续动效工具类**：`text-shimmer`（等待文案微光）、`animate-breathe`（活跃状态点呼吸），只用于「系统正在干活」的语义，不做纯装饰。
- **reduced-motion 是硬规则**：位移/缩放类动效必须给 `motion-reduce:` 降级（保留透明度渐变，去掉移动）；工具类需自带降级（`text-shimmer` 已内置）。

### 5.5 交互模式

- **创建核心对象走"进入即用"（draft-first），不走表单**：像 ChatGPT/Claude 的新建会话——直接进入空白目标页，参数（agent/工作目录）在输入框旁用胶囊控件就地选、可不选用默认，**首次实质动作才真正创建**（参考 `routes/session-chat.tsx` 的草稿态：首条消息落地才建会话，标题由后端从首条消息自动简写）。不要让用户在见到东西之前先填表。
- **轻量配置操作才用 `Dialog`**：需要几个字段确认、且完成后要回到原地的操作（改名、危险确认等）原地弹出；内容本身是"一整页"（对话流、详情、列表）才配路由。
- **整行可点**：表格/列表行的主链接用拉伸链接模式（行 `relative` + 链接 `after:absolute after:inset-0`），语义保持 `<a>`。
- **承载功能的行内操作常显**（删除、引用等）：用 `text-muted-foreground` 压低存在感、hover 时才上色，但**不要用 `opacity-0` + `group-hover` 藏起来**。藏起来的按钮就是「hover 专属效果承载了唯一信息」，与下面的可访问性一条直接冲突；而且 macOS 桌面壳的 WKWebView 里 hover 态并不总跟着鼠标走，藏了就等于点不到。纯装饰的提示（可编辑的铅笔、指示方向的箭头）不承载信息，照旧可以 hover 才现。
- **危险操作**：一律 `AlertDialog` 确认，确认按钮 `variant="destructive"`，文案讲清后果（如"子进程会一并回收，记录不可恢复"）。
- **时间显示**：列表用相对时间（`lib/format.ts`），`title` 悬停给完整时间；时间与数字列加 `tabular-nums`。
- **可访问性**：可点击元素用 `<button>` / `<a>`（或 shadcn 等价物），不给 `div` 挂 onClick；表单控件有 label；hover 专属效果不承载唯一信息。
- **响应式**：移动端判断用 `use-mobile.ts`，断点用 `md:` 等前缀；容器查询（`@container`）优先于视口断点做卡片级适配。侧边栏既有响应式行为不破坏。

### 5.6 页面验收清单

新页面 / 改版合入前逐条自查：

1. loading / error / empty 三态齐全，empty 带下一步 CTA；
2. light / dark × 当前 palette 下都检查过（至少 forest + graphite 两套）；
3. 所有文案走 `t()`，zh/en 双语齐；
4. 无硬编码色值、无原生 confirm/alert；轻量创建/编辑走 Dialog 不切页；
5. 动效符合 §5.4（时长/easing/reduced-motion）；
6. 命令与路径 `font-mono`，数字列 `tabular-nums`，图标 `size-4` 基线对齐；
7. 键盘走查一遍：focus 可见、次级操作可达、弹窗可 Esc。
