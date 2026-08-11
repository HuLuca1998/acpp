import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
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
