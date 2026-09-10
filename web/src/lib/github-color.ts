import type { GithubColor } from "@/types/github"

/**
 * GitHub 单选字段的颜色名 → 状态点的底色类。颜色本身是 index.css 里的
 * `--gh-*` token（跨 palette 恒定，明暗各一版），这里只做名字到类名的映射，
 * 组件里不写色值。空串（没读到字段）落到灰。
 */
export function githubDotClass(color: GithubColor | ""): string {
  switch (color) {
    case "BLUE":
      return "bg-gh-blue"
    case "GREEN":
      return "bg-gh-green"
    case "YELLOW":
      return "bg-gh-yellow"
    case "ORANGE":
      return "bg-gh-orange"
    case "RED":
      return "bg-gh-red"
    case "PINK":
      return "bg-gh-pink"
    case "PURPLE":
      return "bg-gh-purple"
    default:
      return "bg-gh-gray"
  }
}
