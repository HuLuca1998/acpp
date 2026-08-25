/**
 * 复制到剪贴板，返回是否成功。
 *
 * 网页版常见的坑：`navigator.clipboard` 只在安全上下文（https 或
 * localhost）里存在，局域网 http 访问时整个对象都是 undefined；即便存在，
 * 页面失焦或权限被拒也会抛。所以先试异步 API，失败退回 `execCommand`——
 * 它虽被废弃，但不要求安全上下文，且所有浏览器仍支持。
 */
export async function copyText(value: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return true
    } catch {
      // 权限被拒 / 文档失焦，往下走兜底。
    }
  }
  return legacyCopy(value)
}

/** `execCommand("copy")` 兜底：临时 textarea 选中再复制，用完还原选区。 */
function legacyCopy(value: string): boolean {
  const el = document.createElement("textarea")
  el.value = value
  el.setAttribute("readonly", "")
  // 不能用 display:none/visibility:hidden——那样选不中。挪出视口即可，
  // 定位用 fixed 是为了不触发页面滚动。
  el.style.position = "fixed"
  el.style.top = "0"
  el.style.left = "-9999px"
  document.body.appendChild(el)

  const selection = document.getSelection()
  const previous =
    selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null

  try {
    el.select()
    el.setSelectionRange(0, value.length)
    return document.execCommand("copy")
  } catch {
    return false
  } finally {
    el.remove()
    // 复制不该把用户原本划的选区吃掉。
    if (selection && previous) {
      selection.removeAllRanges()
      selection.addRange(previous)
    }
  }
}
