import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    // 产物统一进顶层 build/（根 AGENTS.md §1.1），与后端二进制并列。
    outDir: "../build/web",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // 页面本身已按路由懒加载（见 App.tsx），这里只钉几个**大依赖的
        // 归属**，为的是更新时的缓存命中：桌面版一键更新会换掉全部产物，
        // 把不随业务变化的库固定在自己的块里，改一行界面不会让它跟着换哈希。
        //
        // 只给「大且明确属于某一场景」的库分组，不设兜底组——实测兜底组会
        // 把只有某一页用得到的东西（表格、命令面板）一起拽进首屏。同理
        // 图表库刻意**不**单独分组：给它建组反而会让整块被提升成入口的静态
        // 依赖，369KB 的 recharts 就跟着首屏走了；留给打包器按「谁真的用到」
        // 自动切，它才安静地待在概览页的分片里。
        advancedChunks: {
          groups: [
            {
              name: "react",
              test: /node_modules\/(react|react-dom|react-router|scheduler)\//,
            },
            // 终端：只在开出终端面板时动态载入，330KB 不进首屏。
            { name: "xterm", test: /node_modules\/@xterm\// },
            // 停靠框架：只有会话工作区用。
            { name: "dock", test: /node_modules\/dockview/ },
            // markdown 解析链：对话与技能详情用。
            {
              name: "markdown",
              test: /node_modules\/(react-markdown|remark-|micromark|mdast-|hast-|unist-|vfile|unified|devlop|trough|bail|zwitch|longest-streak|ccount|markdown-table|trim-lines|property-information|space-separated-tokens|comma-separated-tokens|character-entities|decode-named-character|html-url-attributes|is-plain-obj|estree-util|@types\/(mdast|hast|unist|estree))/,
            },
            // 其余依赖不设兜底组：让打包器按「谁真的用到」自动切，
            // 一个兜底大块会把只有某一页用得到的东西（表格、命令面板）
            // 一起拽进首屏。
          ],
        },
      },
    },
  },
  resolve: {
    alias: {
      "@": new URL("./src", import.meta.url).pathname,
    },
  },
  server: {
    // 端口是项目固定约定（见根 AGENTS.md §4.0），改动要连后端与脚本一起改。
    port: 45173,
    proxy: {
      // 开发期把 /api 转发到 Go 后端，避免 CORS；ws 供工作区终端。
      "/api": {
        target: "http://127.0.0.1:48080",
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
