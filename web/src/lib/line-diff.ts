export type DiffLine = { type: "same" | "del" | "add"; text: string }

/** 行级 diff：LCS 对齐；超大文件退化为整删整增，避免 O(n·m) 爆内存。 */
export function lineDiff(oldText: string, newText: string): DiffLine[] {
  const a = oldText === "" ? [] : oldText.replace(/\n$/, "").split("\n")
  const b = newText === "" ? [] : newText.replace(/\n$/, "").split("\n")

  if (a.length * b.length > 250_000) {
    return [
      ...a.map((text) => ({ type: "del" as const, text })),
      ...b.map((text) => ({ type: "add" as const, text })),
    ]
  }

  // LCS 动态规划表
  const dp: number[][] = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0)
  )
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      dp[i][j] =
        a[i] === b[j]
          ? dp[i + 1][j + 1] + 1
          : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }

  const lines: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      lines.push({ type: "same", text: a[i] })
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      lines.push({ type: "del", text: a[i] })
      i++
    } else {
      lines.push({ type: "add", text: b[j] })
      j++
    }
  }
  while (i < a.length) lines.push({ type: "del", text: a[i++] })
  while (j < b.length) lines.push({ type: "add", text: b[j++] })
  return lines
}
