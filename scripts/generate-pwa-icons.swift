// Run from the repo root: swift scripts/generate-pwa-icons.swift
// macOS CoreText + rsvg-convert. Outlines use the existing self-hosted font;
// the resulting SVG/PNG assets need no installed fonts at runtime.
import Foundation
import CoreText
import CoreGraphics

let root = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
let fontURL = root.appendingPathComponent("static/fonts/cormorant-garamond-latin-italic.woff2")
guard let descriptors = CTFontManagerCreateFontDescriptorsFromURL(fontURL as CFURL) as? [CTFontDescriptor],
      let descriptor = descriptors.first(where: {
          (CTFontDescriptorCopyAttribute($0, kCTFontNameAttribute) as? String) == "CormorantGaramond-Italic"
      }) else { fatalError("Existing Cormorant Garamond italic font unavailable") }
let font = CTFontCreateWithFontDescriptor(descriptor, 1000, nil)
let output = root.appendingPathComponent("static/icons")
try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)

func number(_ value: CGFloat) -> String {
    String(format: "%.3f", locale: Locale(identifier: "en_US_POSIX"), Double(value))
}

func outline(_ text: String) -> CGPath {
    let characters = Array(text.utf16)
    var glyphs = Array(repeating: CGGlyph(), count: characters.count)
    guard CTFontGetGlyphsForCharacters(font, characters, &glyphs, characters.count) else {
        fatalError("Missing brand glyph")
    }
    var advances = Array(repeating: CGSize.zero, count: glyphs.count)
    CTFontGetAdvancesForGlyphs(font, .horizontal, glyphs, &advances, glyphs.count)
    let result = CGMutablePath()
    var cursor: CGFloat = 0
    for (index, glyph) in glyphs.enumerated() {
        if let path = CTFontCreatePathForGlyph(font, glyph, nil) {
            result.addPath(path, transform: CGAffineTransform(translationX: cursor, y: 0))
        }
        cursor += advances[index].width
    }
    return result
}

func pathData(_ path: CGPath) -> String {
    var pieces: [String] = []
    path.applyWithBlock { pointer in
        let item = pointer.pointee
        func p(_ index: Int) -> String { number(item.points[index].x) + " " + number(item.points[index].y) }
        switch item.type {
        case .moveToPoint: pieces.append("M" + p(0))
        case .addLineToPoint: pieces.append("L" + p(0))
        case .addQuadCurveToPoint: pieces.append("Q" + p(0) + " " + p(1))
        case .addCurveToPoint: pieces.append("C" + p(0) + " " + p(1) + " " + p(2))
        case .closeSubpath: pieces.append("Z")
        @unknown default: fatalError("Unsupported glyph path")
        }
    }
    return pieces.joined(separator: " ")
}

func svg(_ text: String, width: CGFloat, height: CGFloat, rounded: Bool) -> String {
    let path = outline(text)
    let bounds = path.boundingBoxOfPath
    let scale = min(width / bounds.width, height / bounds.height)
    let x = 256 - bounds.midX * scale
    let y = 256 + bounds.midY * scale
    // App wordmark fits within the central maskable safe circle (radius 204.8).
    return """
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="512" height="512">
      <title>YUHADA HAIR</title>
      <rect width="512" height="512" rx="\(rounded ? "104" : "0")" fill="#F0E4D0"/>
      <path fill="#1A1614" transform="matrix(\(number(scale)) 0 0 \(number(-scale)) \(number(x)) \(number(y)))" d="\(pathData(path))"/>
    </svg>
    """
}

try svg("yhd", width: 316, height: 250, rounded: false).write(to: output.appendingPathComponent("icon.svg"), atomically: true, encoding: .utf8)
try svg("y", width: 250, height: 334, rounded: true).write(to: output.appendingPathComponent("favicon.svg"), atomically: true, encoding: .utf8)
for (input, name, size) in [
    ("icon.svg", "icon-192.png", 192), ("icon.svg", "icon-512.png", 512),
    ("icon.svg", "icon-maskable-512.png", 512), ("icon.svg", "apple-touch-icon.png", 180),
    ("favicon.svg", "favicon-32.png", 32), ("favicon.svg", "favicon-16.png", 16)
] {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    process.arguments = ["rsvg-convert", "-w", String(size), "-h", String(size),
        "-o", output.appendingPathComponent(name).path, output.appendingPathComponent(input).path]
    try process.run()
    process.waitUntilExit()
    guard process.terminationStatus == 0 else { fatalError("PNG rendering failed") }
}

func appendLE(_ value: UInt32, bytes: Int, into data: inout Data) {
    for shift in 0..<bytes { data.append(UInt8((value >> (shift * 8)) & 255)) }
}
let sizes = [16, 32]
let images = try sizes.map { try Data(contentsOf: output.appendingPathComponent("favicon-\($0).png")) }
var ico = Data([0, 0, 1, 0, 2, 0])
var offset = UInt32(6 + sizes.count * 16)
for (index, size) in sizes.enumerated() {
    ico.append(contentsOf: [UInt8(size), UInt8(size), 0, 0])
    appendLE(1, bytes: 2, into: &ico)
    appendLE(32, bytes: 2, into: &ico)
    appendLE(UInt32(images[index].count), bytes: 4, into: &ico)
    appendLE(offset, bytes: 4, into: &ico)
    offset += UInt32(images[index].count)
}
images.forEach { ico.append($0) }
try ico.write(to: output.appendingPathComponent("favicon.ico"))
print("Generated outlined brand SVG, 192/512/maskable/Apple PNGs and 16/32 favicon")
