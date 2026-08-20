import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { BrowserRouter } from "react-router"

import "./index.css"
import "./i18n"
import App from "./App.tsx"
import { ThemeProvider } from "@/components/shell/theme-provider.tsx"
import { IdentityProvider } from "@/components/shell/identity-provider"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { applyPalette, loadPalette } from "@/lib/palette"

// 首帧渲染前应用主题方案，避免默认配色一闪而过。
applyPalette(loadPalette())

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      {/* 图标按钮的说明气泡（components/hint.tsx）走这一个 provider：
          稍等一下再弹，免得划过工具条就一路炸开；相邻按钮之间（400ms 内）
          则是瞬开，扫一排图标不用每个都等。 */}
      <TooltipProvider delay={350} closeDelay={100}>
        {/* 身份要在路由之上：邀请链接是 `/?invite=<token>`，兑换发生在
            任何页面渲染之前（adr-007）。 */}
        <IdentityProvider>
          <BrowserRouter>
            <App />
            <Toaster />
          </BrowserRouter>
        </IdentityProvider>
      </TooltipProvider>
    </ThemeProvider>
  </StrictMode>
)
