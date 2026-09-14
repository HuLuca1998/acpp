# grok-ball（第三方，禁止手改）

输入卡吉祥物的渲染引擎：纯 SVG、零运行依赖，一双眼睛跟随鼠标，内置 32 种
表情（生命周期 / 情绪反应 / 代理工作状态三组）。

- 上游：https://github.com/tycoding/grok-ball
- 许可：MIT（见 [LICENSE](LICENSE)，保留原作者版权声明）
- 取自 commit `de368ce3acdc5a70e871ee46ebbe8f60b11d3d6a`（2026-08-20）

## 目录里是什么

| 文件 | 来源 | 说明 |
| --- | --- | --- |
| `grok-ball.js` | 上游 `src/grok-ball.js`，**逐字未改** | 浏览器引擎（IIFE，挂 `window.GrokBall`） |
| `index.ts` | 上游 `src/grok-ball.ts` | TS 类型入口，`import "./grok-ball.js"` 后把 `window.GrokBall` 暴露成具名导出 |
| `LICENSE` | 上游 | MIT |

## 规矩

- **禁止手改 `grok-ball.js`**：它是上游产物，改了下次升级会被整体覆盖，且改动无处追溯。
  要调行为一律通过 `create()` 的入参、`registerEmotion()` 或我们自己那一层
  （[components/chat/composer/mascot.tsx](../../components/chat/composer/mascot.tsx)）解决。
- **不引入上游的 `index.html` / `scripts/`**：那是上游的演示页与构建工具，与本项目无关。
- 颜色不在这里写死：球体与眼睛的颜色由 `--mascot` / `--mascot-eye` 两个语义 token 决定
  （定义在 `web/src/index.css`），由 mascot.tsx 在挂载时读进 `create()`。

## 怎么升级

1. 对着上游同名文件取新版，覆盖 `grok-ball.js` 与 `index.ts`（**只这两个**）。
2. 更新本文件里的 commit 与日期。
3. 跑一遍输入卡的表情：`make dev` 后开一个会话发一句话，确认
   思考 / 干活 / 查资料 / 回复 / 完成几个态都还在动（表情 id 见 mascot.tsx 的映射表）。
