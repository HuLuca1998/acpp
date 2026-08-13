// 用途：程序化绘制 ACP Console 的全套图标——App 图标 iconset（Dock/访达用）
// 与菜单栏模板图。图标即代码，仓库不存二进制，打包时现画（可安全重跑）。
// 用法：swift icongen.swift <输出目录>     （由 scripts/build-macos-app.sh 调用）
// 前置：macOS + Xcode Command Line Tools（AppKit / CoreGraphics / iconutil）。
//
// 设计语言：终端提示符 ❯ + 光标下划线，绿色取自 web 主题 primary（绿 150° 系），
// 深色底板贴合控制台气质；菜单栏图是纯黑模板图，交给系统按明暗自动着色。

import AppKit
import UniformTypeIdentifiers

// MARK: - 画布与输出

func makeContext(_ px: Int) -> CGContext {
    let space = CGColorSpace(name: CGColorSpace.sRGB)!
    return CGContext(
        data: nil, width: px, height: px, bitsPerComponent: 8, bytesPerRow: 0,
        space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
    )!
}

func writePNG(_ image: CGImage, to url: URL) {
    guard let dest = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil) else {
        fatalError("无法创建 PNG 输出：\(url.path)")
    }
    CGImageDestinationAddImage(dest, image, nil)
    guard CGImageDestinationFinalize(dest) else { fatalError("PNG 写入失败：\(url.path)") }
}

func rgba(_ hex: UInt32, _ alpha: CGFloat = 1) -> CGColor {
    CGColor(
        srgbRed: CGFloat((hex >> 16) & 0xFF) / 255,
        green: CGFloat((hex >> 8) & 0xFF) / 255,
        blue: CGFloat(hex & 0xFF) / 255,
        alpha: alpha
    )
}

// MARK: - App 图标（矢量逐尺寸重绘，小尺寸不糊）

func drawAppIcon(px: Int) -> CGImage {
    let s = CGFloat(px)
    let ctx = makeContext(px)

    // Apple 图标模板比例：squircle 占 1024 中的 832，四周留投影呼吸位。
    let side = s * 832.0 / 1024.0
    let origin = (s - side) / 2
    let radius = side * 0.2237
    let rect = CGRect(x: origin, y: origin, width: side, height: side)
    let squircle = CGPath(roundedRect: rect, cornerWidth: radius, cornerHeight: radius, transform: nil)

    // 底板投影
    ctx.saveGState()
    ctx.setShadow(offset: CGSize(width: 0, height: -s * 0.008), blur: s * 0.02, color: CGColor(gray: 0, alpha: 0.35))
    ctx.addPath(squircle)
    ctx.setFillColor(rgba(0x0A0F0D))
    ctx.fillPath()
    ctx.restoreGState()

    // 背景：墨绿渐变 + 中心辉光
    ctx.saveGState()
    ctx.addPath(squircle)
    ctx.clip()
    let bg = CGGradient(
        colorsSpace: nil,
        colors: [rgba(0x20302A), rgba(0x0A100D)] as CFArray, locations: [0, 1]
    )!
    ctx.drawLinearGradient(bg, start: CGPoint(x: 0, y: origin + side), end: CGPoint(x: 0, y: origin), options: [])
    let glowCenter = CGPoint(x: s * 0.47, y: s * 0.48)
    let glow = CGGradient(
        colorsSpace: nil,
        colors: [rgba(0x22C55E, 0.22), rgba(0x22C55E, 0)] as CFArray, locations: [0, 1]
    )!
    ctx.drawRadialGradient(glow, startCenter: glowCenter, startRadius: 0, endCenter: glowCenter, endRadius: s * 0.42, options: [])

    // 提示符 ❯：粗圆头折线，带绿色外发光；16px 下保底线宽防止细节消失
    let strokeW = max(s * 0.058, 1.6)
    ctx.setLineCap(.round)
    ctx.setLineJoin(.round)
    ctx.setLineWidth(strokeW)
    ctx.setStrokeColor(rgba(0x4ADE80))
    ctx.setShadow(offset: .zero, blur: s * 0.05, color: rgba(0x34D399, 0.6))
    ctx.beginPath()
    ctx.move(to: CGPoint(x: s * 0.350, y: s * 0.645))
    ctx.addLine(to: CGPoint(x: s * 0.495, y: s * 0.500))
    ctx.addLine(to: CGPoint(x: s * 0.350, y: s * 0.355))
    ctx.strokePath()

    // 光标下划线：亮薄荷色，与提示符底边对齐
    let cursor = CGRect(x: s * 0.550, y: s * 0.355 - strokeW / 2, width: s * 0.135, height: strokeW)
    ctx.setShadow(offset: .zero, blur: s * 0.05, color: rgba(0xA7F3D0, 0.5))
    ctx.setFillColor(rgba(0xD1FAE5))
    ctx.addPath(CGPath(roundedRect: cursor, cornerWidth: strokeW / 2, cornerHeight: strokeW / 2, transform: nil))
    ctx.fillPath()
    ctx.restoreGState()

    // 内侧高光描边，给深色底板一点立体感
    ctx.saveGState()
    ctx.addPath(squircle)
    ctx.clip()
    let inset = max(s * 0.002, 0.5)
    ctx.addPath(CGPath(
        roundedRect: rect.insetBy(dx: inset, dy: inset),
        cornerWidth: radius - inset, cornerHeight: radius - inset, transform: nil
    ))
    ctx.setStrokeColor(CGColor(gray: 1, alpha: 0.07))
    ctx.setLineWidth(max(s * 0.004, 1))
    ctx.strokePath()
    ctx.restoreGState()

    return ctx.makeImage()!
}

// MARK: - 菜单栏模板图（18pt 逻辑画布，纯黑 + alpha）

func drawMenuBarIcon(px: Int) -> CGImage {
    let ctx = makeContext(px)
    let u = CGFloat(px) / 18.0
    ctx.setLineCap(.round)
    ctx.setLineJoin(.round)
    ctx.setStrokeColor(CGColor(gray: 0, alpha: 1))
    ctx.setLineWidth(2.0 * u)
    ctx.beginPath()
    ctx.move(to: CGPoint(x: 3.6 * u, y: 13.2 * u))
    ctx.addLine(to: CGPoint(x: 8.6 * u, y: 9.0 * u))
    ctx.addLine(to: CGPoint(x: 3.6 * u, y: 4.8 * u))
    ctx.strokePath()
    let bar = CGRect(x: 10.4 * u, y: 4.0 * u, width: 4.4 * u, height: 1.9 * u)
    ctx.setFillColor(CGColor(gray: 0, alpha: 1))
    ctx.addPath(CGPath(roundedRect: bar, cornerWidth: 0.95 * u, cornerHeight: 0.95 * u, transform: nil))
    ctx.fillPath()
    return ctx.makeImage()!
}

// MARK: - 入口

let args = CommandLine.arguments
guard args.count == 2 else {
    FileHandle.standardError.write(Data("用法：swift icongen.swift <输出目录>\n".utf8))
    exit(2)
}
let outDir = URL(fileURLWithPath: args[1], isDirectory: true)
let iconset = outDir.appendingPathComponent("AppIcon.iconset", isDirectory: true)
try FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)

// iconutil 认的固定命名：icon_<pt>x<pt>[@2x].png
let iconsetSpecs: [(name: String, px: Int)] = [
    ("icon_16x16", 16), ("icon_16x16@2x", 32),
    ("icon_32x32", 32), ("icon_32x32@2x", 64),
    ("icon_128x128", 128), ("icon_128x128@2x", 256),
    ("icon_256x256", 256), ("icon_256x256@2x", 512),
    ("icon_512x512", 512), ("icon_512x512@2x", 1024),
]
for spec in iconsetSpecs {
    writePNG(drawAppIcon(px: spec.px), to: iconset.appendingPathComponent("\(spec.name).png"))
}

writePNG(drawMenuBarIcon(px: 18), to: outDir.appendingPathComponent("MenuBarIcon.png"))
writePNG(drawMenuBarIcon(px: 36), to: outDir.appendingPathComponent("MenuBarIcon@2x.png"))
// 预览图给人看，尺寸取 512 足够审阅细节
writePNG(drawAppIcon(px: 512), to: outDir.appendingPathComponent("AppIcon-preview.png"))

print("图标已生成：\(outDir.path)")
