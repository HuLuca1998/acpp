/**
 * 复制到剪贴板。非安全上下文（局域网 http 访问就是这种情况）里
 * `navigator.clipboard` 不可用——静默退让，不为一个顺手的小动作弹错误。
 */
export async function copyText(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value)
  } catch {
    // 无声失败是刻意的：调用方都是「复制名字/路径/sha」这类辅助动作。
  }
}
