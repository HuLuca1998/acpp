/** 完整主题方案。全套语义 token 定义在 index.css 的 data-palette 块里，
 * 每套含 light / dark 两版，与明暗切换正交；这里只管状态与持久化。 */

export const PALETTES = ["forest", "bay", "dusk", "sand", "graphite"] as const

export type Palette = (typeof PALETTES)[number]

/** 默认方案（森林）不挂属性，即 index.css 的 :root / .dark 基础值。 */
export const DEFAULT_PALETTE: Palette = "forest"

const STORAGE_KEY = "palette"

/** 切换器预览用的双色 swatch：纸面色 + 主色（取 light 版即可）。 */
export const PALETTE_SWATCHES: Record<
  Palette,
  { surface: string; primary: string }
> = {
  forest: { surface: "oklch(0.985 0 0)", primary: "oklch(0.527 0.154 150.1)" },
  bay: { surface: "oklch(0.975 0.006 240)", primary: "oklch(0.546 0.215 262.9)" },
  dusk: { surface: "oklch(0.975 0.008 300)", primary: "oklch(0.541 0.247 293)" },
  sand: { surface: "oklch(0.97 0.012 85)", primary: "oklch(0.6 0.155 45)" },
  graphite: { surface: "oklch(1 0 0)", primary: "oklch(0.3 0 0)" },
}

export function loadPalette(): Palette {
  const stored = localStorage.getItem(STORAGE_KEY)
  return PALETTES.includes(stored as Palette)
    ? (stored as Palette)
    : DEFAULT_PALETTE
}

export function applyPalette(palette: Palette) {
  const root = document.documentElement
  // 清掉旧版本遗留的强调色属性，避免样式叠加。
  delete root.dataset.accent
  if (palette === DEFAULT_PALETTE) {
    delete root.dataset.palette
  } else {
    root.dataset.palette = palette
  }
}

export function savePalette(palette: Palette) {
  localStorage.setItem(STORAGE_KEY, palette)
  applyPalette(palette)
}
