// 用途：程序化绘制 ACPP 的全套图标——App 图标 iconset（Dock/访达用）
// 与菜单栏模板图。图标即代码，仓库不存二进制，打包时现画（可安全重跑）。
// 用法：swift icongen.swift <输出目录>     （由 scripts/build-macos-app.sh 调用）
// 前置：macOS + Xcode Command Line Tools（AppKit / CoreGraphics / iconutil）。
//
// 设计语言：对话气泡内嵌提示符 ❯_（与 agent 对话），绿色取自 web 主题
// primary（绿 150° 系）；菜单栏图是纯黑模板图，交给系统按明暗自动着色。
// 方案是用户从五个概念稿中选定的 E 款（终端提示符/协议双向流/编排中枢/
// 字母A花押/对话气泡），web 端 public/app-icon.svg 与此同源，改任一侧要对齐。

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

    // 对话气泡：圆角轮廓 + 左下尾巴，绿色外发光；16px 下保底线宽防止细节消失
    let strokeW = max(s * 0.045, 1.4)
    ctx.setLineCap(.round)
    ctx.setLineJoin(.round)
    ctx.setLineWidth(strokeW)
    ctx.setStrokeColor(rgba(0x4ADE80))
    ctx.setShadow(offset: .zero, blur: s * 0.04, color: rgba(0x34D399, 0.6))
    let bubble = CGRect(x: s * 0.300, y: s * 0.400, width: s * 0.400, height: s * 0.280)
    ctx.addPath(CGPath(roundedRect: bubble, cornerWidth: s * 0.09, cornerHeight: s * 0.09, transform: nil))
    ctx.strokePath()
    ctx.beginPath()
    ctx.move(to: CGPoint(x: s * 0.385, y: s * 0.400))
    ctx.addLine(to: CGPoint(x: s * 0.345, y: s * 0.310))
    ctx.addLine(to: CGPoint(x: s * 0.465, y: s * 0.400))
    ctx.strokePath()

    // 气泡内的提示符 ❯_：薄荷色，点出「对话对象是 agent 终端」
    let inner = max(s * 0.040, 1.2)
    ctx.setLineWidth(inner)
    ctx.setStrokeColor(rgba(0xD1FAE5))
    ctx.setShadow(offset: .zero, blur: s * 0.035, color: rgba(0xA7F3D0, 0.5))
    ctx.beginPath()
    ctx.move(to: CGPoint(x: s * 0.405, y: s * 0.600))
    ctx.addLine(to: CGPoint(x: s * 0.470, y: s * 0.540))
    ctx.addLine(to: CGPoint(x: s * 0.405, y: s * 0.480))
    ctx.strokePath()
    ctx.setFillColor(rgba(0xD1FAE5))
    ctx.addPath(CGPath(
        roundedRect: CGRect(x: s * 0.510, y: s * 0.480 - s * 0.020, width: s * 0.085, height: s * 0.040),
        cornerWidth: s * 0.020, cornerHeight: s * 0.020, transform: nil))
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

    // 气泡轮廓 + 左下尾巴
    ctx.setLineWidth(1.7 * u)
    let bubble = CGRect(x: 2.6 * u, y: 5.2 * u, width: 12.8 * u, height: 9.2 * u)
    ctx.addPath(CGPath(roundedRect: bubble, cornerWidth: 2.6 * u, cornerHeight: 2.6 * u, transform: nil))
    ctx.strokePath()
    ctx.beginPath()
    ctx.move(to: CGPoint(x: 5.4 * u, y: 5.2 * u))
    ctx.addLine(to: CGPoint(x: 4.4 * u, y: 2.8 * u))
    ctx.addLine(to: CGPoint(x: 8.0 * u, y: 5.2 * u))
    ctx.strokePath()

    // 气泡内提示符 ❯_（18px 下光标退化成点也仍可读）
    ctx.setLineWidth(1.5 * u)
    ctx.beginPath()
    ctx.move(to: CGPoint(x: 5.4 * u, y: 11.6 * u))
    ctx.addLine(to: CGPoint(x: 7.4 * u, y: 9.8 * u))
    ctx.addLine(to: CGPoint(x: 5.4 * u, y: 8.0 * u))
    ctx.strokePath()
    let bar = CGRect(x: 8.8 * u, y: 8.0 * u, width: 3.2 * u, height: 1.5 * u)
    ctx.setFillColor(CGColor(gray: 0, alpha: 1))
    ctx.addPath(CGPath(roundedRect: bar, cornerWidth: 0.75 * u, cornerHeight: 0.75 * u, transform: nil))
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
