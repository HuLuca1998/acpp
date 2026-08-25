import js from "@eslint/js"
import globals from "globals"

/**
 * 壳的 lint。主进程是 ESM + Node 环境，preload 是 CJS 且同时够得着 window
 * （contextBridge 注入那一侧）——两套 globals 分开声明，否则不是漏报就是误报。
 *
 * 这里没有 TypeScript（见 adr-015：壳是薄装配层，不值得单拉一条 tsc 构建链），
 * lint 因此是唯一挡低级错误的关口，别把它从 make check 里摘掉。
 */
export default [
  { ignores: ["node_modules/**"] },
  js.configs.recommended,
  {
    files: ["main/**/*.js", "*.js"],
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: "module",
      globals: { ...globals.node },
    },
  },
  {
    files: ["preload/**/*.cjs"],
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: "commonjs",
      globals: { ...globals.node, ...globals.browser },
    },
  },
]
