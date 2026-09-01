/**
 * 验证方式 → 文案键。选择器与列表都要用它把 `key` 显示成「公钥」。
 *
 * 单独一个文件而不是放在对话框里：组件文件只导出组件，否则 react-refresh
 * 的热更新会整页刷新（eslint 的 only-export-components 就是在守这条）。
 *
 * 返回字面量而不是 string：i18n 的键是类型化的，写错会当场编译失败。
 */
export function authLabelKey(auth: string) {
  if (auth === "key") return "server.authKey" as const
  if (auth === "both") return "server.authBoth" as const
  return "server.authPassword" as const
}
