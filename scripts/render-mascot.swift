import AppKit
import Foundation
import ImageIO

// Animate one immutable illustration. Every frame is sampled from the same
// continuous rig, not generated from a different pose or optical flow.
guard CommandLine.arguments.count >= 4,
      let size = Int(CommandLine.arguments[1]), size > 0,
      let fps = Int(CommandLine.arguments[2]), fps > 0 else {
    fputs("Usage: swift scripts/render-mascot.swift SIZE FPS OUTPUT_DIRECTORY [FRAME_LIMIT]\n", stderr)
    exit(1)
}
let root = URL(fileURLWithPath: CommandLine.arguments[0])
    .deletingLastPathComponent().deletingLastPathComponent()
    .appendingPathComponent("docs/assets/branding")
let directory = URL(fileURLWithPath: CommandLine.arguments[3])
let limit = CommandLine.arguments.count > 4 ? Int(CommandLine.arguments[4])! : fps * 4
try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
func loadImage(_ name: String) -> CGImage {
    let url = root.appendingPathComponent(name)
    let source = CGImageSourceCreateWithURL(url as CFURL, nil)!
    return CGImageSourceCreateImageAtIndex(source, 0, nil)!
}
let sprite = loadImage("mascot-character-source.png")
let background = loadImage("mascot-tile-source.png")
let width = CGFloat(size)
let duration = 4.0

// Separate the feet at their occluded attachment line. They move behind the
// intact body, so lifting a foot cannot pull a crease into the belly silhouette.
enum Foot { case left, right }
func bodyEdge(_ x: Double) -> Double { 0.198 + 0.90 * pow(x - 0.5, 2) }
func footLayer(_ side: Foot) -> CGImage {
    let pixels = sprite.width
    let n = CGFloat(pixels)
    let space = CGColorSpace(name: CGColorSpace.sRGB)!
    let context = CGContext(data: nil, width: pixels, height: pixels,
                            bitsPerComponent: 8, bytesPerRow: pixels * 4,
                            space: space,
                            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.setShouldAntialias(false)
    let start = side == .left ? 0.0 : 0.5
    let end = start + 0.5
    let path = CGMutablePath()
    path.move(to: CGPoint(x: start * Double(n), y: 0))
    path.addLine(to: CGPoint(x: end * Double(n), y: 0))
    for i in 0...100 {
        let x = end - Double(i) / 200
        path.addLine(to: CGPoint(x: x * Double(n), y: bodyEdge(x) * Double(n)))
    }
    path.closeSubpath()
    context.addPath(path); context.clip()
    context.draw(sprite, in: CGRect(x: 0, y: 0, width: n, height: n))
    return context.makeImage()!
}
let bodySprite = loadImage("mascot-body-source.png")
precondition(bodySprite.width == sprite.width && bodySprite.height == sprite.height,
             "Body and character sources must use the same canvas")
let leftFootSprite = footLayer(.left)
let rightFootSprite = footLayer(.right)

func clamp(_ x: Double) -> Double { max(0, min(1, x)) }
func smooth(_ lo: Double, _ hi: Double, _ x: Double) -> Double {
    let u = clamp((x - lo) / (hi - lo))
    return u * u * (3 - 2 * u)
}
struct Pose {
    var x = 0.0, height = 0.0, stretch = 1.0
    var leftLift = 0.0, rightLift = 0.0, lean = 0.0
    var waveBase = 0.0, waveTip = 0.0
}
func pose(_ time: Double) -> Pose {
    let t = time.truncatingRemainder(dividingBy: duration)
    var p = Pose()
    if t < 1.6 {
        let returning = t >= 0.8
        let local = returning ? t - 0.8 : t
        let flight = clamp((local - 0.12) / 0.56)
        let transfer = smooth(0, 1, flight)
        let direction = returning ? -1.0 : 1.0
        p.x = returning ? 0.022 - 0.044 * transfer : -0.022 + 0.044 * transfer
        p.lean = direction * (transfer - 0.5) * 0.030
        p.leftLift = (returning ? 1 - transfer : transfer) * 0.020
        p.rightLift = (returning ? transfer : 1 - transfer) * 0.020
        if local < 0.12 {
            p.stretch = 1 - 0.065 * pow(sin(.pi * local / 0.12), 2)
        } else if local < 0.68 {
            p.height = 0.075 * sin(.pi * flight)
            p.stretch = 1 + 0.045 * sin(.pi * flight)
        } else {
            p.stretch = 1 - 0.080 * pow(sin(.pi * (local - 0.68) / 0.12), 2)
        }
    } else {
        p.x = -0.022
        p.rightLift = 0.020 * (1 - smooth(1.6, 1.82, t) + smooth(3.70, 4, t))
        let envelope = smooth(1.75, 2.05, t) * (1 - smooth(3.40, 3.80, t))
        p.waveBase = -0.23 * envelope
        let wave = (t - 1.9) / 1.6
        p.waveTip = -0.60 * envelope * (0.5 - 0.5 * cos(6 * .pi * wave))
        p.lean = -0.015 * (1 - smooth(1.6, 1.9, t) + smooth(3.70, 4, t))
    }
    return p
}

// Warm up a damped spring for periodic secondary motion at both ears.
let simulationHz = 600.0
let samples = Int(duration * simulationHz)
var leftAngles = [Double](repeating: 0, count: samples)
var rightAngles = leftAngles
var left = 0.0, leftVelocity = 0.0, right = 0.0, rightVelocity = 0.0
for step in 0..<(samples * 8) {
    let t = Double(step % samples) / simulationHz
    let dt = 1 / simulationHz
    let before = pose((t - dt + duration).truncatingRemainder(dividingBy: duration))
    let current = pose(t)
    let verticalVelocity = (current.height - before.height) / dt
    let horizontalVelocity = (current.x - before.x) / dt
    let leftTarget = 0.42 * verticalVelocity - 0.55 * horizontalVelocity
    let rightTarget = -0.26 * verticalVelocity - 0.35 * horizontalVelocity
    leftVelocity += (145 * (leftTarget - left) - 8.4 * leftVelocity) * dt
    left += leftVelocity * dt
    rightVelocity += (185 * (rightTarget - right) - 10 * rightVelocity) * dt
    right += rightVelocity * dt
    if step >= samples * 7 {
        leftAngles[step % samples] = left
        rightAngles[step % samples] = right
    }
}
func spring(_ values: [Double], _ t: Double) -> Double {
    let index = t / duration * Double(samples)
    let i = Int(index) % samples
    let mix = index - floor(index)
    return values[i] * (1 - mix) + values[(i + 1) % samples] * mix
}
func rotate(_ p: CGPoint, around anchor: CGPoint, by angle: Double) -> CGPoint {
    let x = Double(p.x - anchor.x), y = Double(p.y - anchor.y)
    return CGPoint(x: Double(anchor.x) + x * cos(angle) - y * sin(angle),
                   y: Double(anchor.y) + x * sin(angle) + y * cos(angle))
}
func vertex(_ x: Double, _ y: Double, _ t: Double, _ p: Pose) -> CGPoint {
    var v = CGPoint(x: x, y: y)
    let leftWeight = smooth(0.57, 0.69, y) * (1 - smooth(0.44, 0.57, x))
    let rightWeight = smooth(0.50, 0.64, y) * smooth(0.53, 0.65, x)
    let leftAngle = spring(leftAngles, t)
    let rightAngle = spring(rightAngles, t)
    let leftRotated = rotate(v, around: CGPoint(x: 0.43, y: 0.63),
                             by: leftAngle + p.waveBase)
    v.x += (leftRotated.x - v.x) * leftWeight
    v.y += (leftRotated.y - v.y) * leftWeight
    let tipWeight = leftWeight * (1 - smooth(0.18, 0.43, x))
    let tipRotated = rotate(v, around: CGPoint(x: 0.32, y: 0.79),
                            by: leftAngle * 0.7 + p.waveTip)
    v.x += (tipRotated.x - v.x) * tipWeight
    v.y += (tipRotated.y - v.y) * tipWeight
    let rightRotated = rotate(v, around: CGPoint(x: 0.62, y: 0.61), by: rightAngle)
    v.x += (rightRotated.x - v.x) * rightWeight
    v.y += (rightRotated.y - v.y) * rightWeight
    let scale = 0.80
    let stretchX = 1 / sqrt(p.stretch)
    let bodyLean = smooth(0.22, 0.58, y) * p.lean
    return CGPoint(x: (0.5 + p.x + (Double(v.x) - 0.5) * scale * stretchX + bodyLean) * Double(width),
                   y: (0.16 + (Double(v.y) - 0.12) * scale * p.stretch + p.height) * Double(width))
}

// Small texture tiles keep rasterization fast while a dense mesh bends the ears.
struct Tile {
    let image: CGImage
    let x: Double, y: Double, w: Double, h: Double
}
let cells = 26
var tiles = [Tile]()
for row in 0..<cells {
    for column in 0..<cells {
        let x0 = Int(Double(column) / Double(cells) * Double(sprite.width))
        let x1 = Int(Double(column + 1) / Double(cells) * Double(sprite.width))
        let y0 = Int(Double(row) / Double(cells) * Double(sprite.height))
        let y1 = Int(Double(row + 1) / Double(cells) * Double(sprite.height))
        let rect = CGRect(x: x0, y: sprite.height - y1, width: x1 - x0, height: y1 - y0)
        let image = bodySprite.cropping(to: rect)!
        tiles.append(Tile(image: image, x: Double(x0) / Double(sprite.width),
                          y: Double(y0) / Double(sprite.height),
                          w: Double(x1 - x0) / Double(sprite.width),
                          h: Double(y1 - y0) / Double(sprite.height)))
    }
}
func drawTriangle(_ context: CGContext, _ image: CGImage,
                  _ a: CGPoint, _ b: CGPoint, _ c: CGPoint,
                  _ transform: CGAffineTransform) {
    guard abs(transform.a * transform.d - transform.b * transform.c) > 0.05 else { return }
    context.saveGState()
    context.beginPath()
    context.move(to: a); context.addLine(to: b); context.addLine(to: c)
    context.closePath(); context.clip()
    context.concatenate(transform)
    context.draw(image, in: CGRect(x: 0, y: 0, width: 1, height: 1))
    context.restoreGState()
}
let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!
for frame in 0..<limit {
    autoreleasepool {
        let time = Double(frame) / Double(fps)
        let p = pose(time)
        let context = CGContext(data: nil, width: size, height: size,
                                bitsPerComponent: 8, bytesPerRow: size * 4,
                                space: colorSpace,
                                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        context.interpolationQuality = .high
        context.saveGState()
        let plate = CGRect(x: width * 0.035, y: width * 0.035,
                           width: width * 0.93, height: width * 0.93)
        context.addPath(CGPath(roundedRect: plate, cornerWidth: width * 0.18,
                               cornerHeight: width * 0.18, transform: nil))
        context.clip()
        context.draw(background, in: CGRect(x: 0, y: 0, width: size, height: size))
        context.restoreGState()
        let shadowWidth = width * (0.34 - p.height * 0.6)
        context.saveGState()
        context.translateBy(x: width * (0.5 + p.x), y: width * 0.158)
        context.scaleBy(x: shadowWidth / 2, y: width * 0.026)
        let shadowColors = [CGColor(gray: 0.02, alpha: 0.32 - p.height),
                            CGColor(gray: 0.02, alpha: 0.11 - p.height / 2),
                            CGColor(gray: 0.02, alpha: 0)] as CFArray
        let shadowGradient = CGGradient(colorsSpace: colorSpace, colors: shadowColors,
                                        locations: [0, 0.55, 1])!
        context.drawRadialGradient(shadowGradient, startCenter: .zero, startRadius: 0,
                                   endCenter: .zero, endRadius: 1, options: [])
        context.restoreGState()

        // Feet retain their original shape and are occluded naturally by the
        // unwarped belly drawn afterward. Only the ear/body rig uses the mesh.
        let footWidth = width * 0.80 / sqrt(p.stretch)
        let footHeight = width * 0.80 * p.stretch
        let footX = width * CGFloat(0.5 + p.x) - footWidth / 2
        let footY = width * CGFloat(0.16 + p.height) - footHeight * 0.12
        for (image, lift) in [(leftFootSprite, p.leftLift), (rightFootSprite, p.rightLift)] {
            context.draw(image, in: CGRect(x: footX, y: footY + width * CGFloat(lift),
                                           width: footWidth, height: footHeight))
        }
        context.setShouldAntialias(false)
        for tile in tiles {
            let a = vertex(tile.x, tile.y, time, p)
            let b = vertex(tile.x + tile.w, tile.y, time, p)
            let c = vertex(tile.x, tile.y + tile.h, time, p)
            let d = vertex(tile.x + tile.w, tile.y + tile.h, time, p)
            drawTriangle(context, tile.image, a, b, c,
                         CGAffineTransform(a: b.x - a.x, b: b.y - a.y,
                                           c: c.x - a.x, d: c.y - a.y,
                                           tx: a.x, ty: a.y))
            drawTriangle(context, tile.image, d, c, b,
                         CGAffineTransform(a: d.x - c.x, b: d.y - c.y,
                                           c: d.x - b.x, d: d.y - b.y,
                                           tx: b.x + c.x - d.x, ty: b.y + c.y - d.y))
        }
        let image = context.makeImage()!
        let path = directory.appendingPathComponent(String(format: "frame-%04d.png", frame))
        let output = CGImageDestinationCreateWithURL(path as CFURL, "public.png" as CFString, 1, nil)!
        CGImageDestinationAddImage(output, image, nil)
        CGImageDestinationFinalize(output)
        if frame % fps == 0 { print("Rendered \(frame)/\(limit) at \(size)px, \(fps) fps") }
    }
}
print("Wrote \(limit) frames to \(directory.path)")
