#!/usr/bin/env swift
// Render native text and the approved mascot without regenerating its artwork.
import AppKit
import Foundation

let root = URL(fileURLWithPath:CommandLine.arguments[0]).deletingLastPathComponent().deletingLastPathComponent()
let assets = root.appendingPathComponent("docs/assets/branding")
let iconURL = assets.appendingPathComponent("xswap-icon.png")
guard let icon = NSImage(contentsOf:iconURL),
      let bitmap = NSBitmapImageRep(bitmapDataPlanes:nil, pixelsWide:1280, pixelsHigh:640,
          bitsPerSample:8, samplesPerPixel:4, hasAlpha:true, isPlanar:false,
          colorSpaceName:.deviceRGB, bytesPerRow:0, bitsPerPixel:0),
      let context = NSGraphicsContext(bitmapImageRep:bitmap) else {
    fatalError("Cannot load mascot or create the social-preview canvas")
}

func color(_ hex:UInt32) -> NSColor {
    NSColor(srgbRed:CGFloat((hex >> 16) & 255)/255,
            green:CGFloat((hex >> 8) & 255)/255,
            blue:CGFloat(hex & 255)/255, alpha:1)
}
func font(_ name:String, _ size:CGFloat) -> NSFont {
    guard let result = NSFont(name:name, size:size) else {
        fatalError("Missing macOS font: \(name)")
    }
    return result
}
func text(_ value:String, x:CGFloat, y:CGFloat, width:CGFloat, height:CGFloat,
          size:CGFloat, ink:UInt32, weight:String = "AvenirNext-Medium") {
    let paragraph = NSMutableParagraphStyle()
    paragraph.lineBreakMode = .byWordWrapping
    NSAttributedString(string:value, attributes:[
        .font:font(weight,size), .foregroundColor:color(ink), .paragraphStyle:paragraph
    ]).draw(in:NSRect(x:x,y:y,width:width,height:height))
}

NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = context
context.imageInterpolation = .high
color(0x0D0C15).setFill()
NSRect(x:0,y:0,width:1280,height:640).fill()

let title = NSMutableAttributedString(string:"XSwap", attributes:[
    .font:font("AvenirNext-Heavy",108), .foregroundColor:color(0xF7F4FF)
])
title.addAttribute(.foregroundColor, value:color(0xBD94FF), range:NSRange(location:0,length:1))
title.draw(in:NSRect(x:80,y:358,width:640,height:145))
text("Account switcher for Codex.",x:86,y:286,width:650,height:64,
     size:32,ink:0xF7F4FF,weight:"AvenirNext-DemiBold")
text("Isolated accounts. Live quotas.\nSession handoff.",x:86,y:180,width:630,height:90,
     size:24,ink:0xBEB6CD)
text("macOS  ·  Linux  ·  Windows",x:86,y:65,width:600,height:36,size:20,ink:0xBEB6CD)

NSGraphicsContext.saveGraphicsState()
let iconRect = NSRect(x:818,y:150,width:376,height:376)
NSBezierPath(roundedRect:iconRect.insetBy(dx:8,dy:8),xRadius:82,yRadius:82).addClip()
icon.draw(in:iconRect,from:.zero,operation:.sourceOver,fraction:1,
          respectFlipped:false,hints:[.interpolation:NSImageInterpolation.high])
NSGraphicsContext.restoreGraphicsState()
NSGraphicsContext.restoreGraphicsState()

guard let png = bitmap.representation(using:.png,properties:[:]) else {
    fatalError("PNG export failed")
}
guard png.count < 1_000_000 else {
    fatalError("Social preview must stay below GitHub's 1 MB limit")
}
let output = assets.appendingPathComponent("github-social-preview.png")
try png.write(to:output)
print("Wrote \(output.path) — 1280×640, \(png.count) bytes")
