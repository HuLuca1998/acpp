import { createContext, useContext, useEffect } from "react"

/**
 * 页面把自己的标题报给外壳顶栏。
 *
 * 路由表只知道「这是会话页」，报不出「这是哪一条会话」——而顶栏要显示的恰恰
 * 是后者（规范 §5.6：顶栏常驻，就该说清楚当前在看什么）。数据在页面手里，
 * 于是由页面上报，不让外壳为了一个标题再拉一次接口。
 *
 * 报 null 或不报，顶栏回落到路由表给的静态标题。
 */
export const PageTitleContext = createContext<(title: string | null) => void>(
  () => {}
)

/** 在页面组件里调用；卸载时自动撤回，换页不会留下上一页的标题。 */
export function usePageTitle(title: string | null | undefined) {
  const report = useContext(PageTitleContext)
  useEffect(() => {
    report(title ?? null)
    return () => report(null)
  }, [title, report])
}
