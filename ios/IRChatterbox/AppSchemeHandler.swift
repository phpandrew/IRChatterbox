import Foundation
import WebKit
import UniformTypeIdentifiers

/// Serves the bundled frontend (the `web` folder, built from source/frontend) at
/// icb://localhost/, so the page has its own origin and ES modules load the same way they do
/// from Wails on Windows and WebViewAssetLoader on Android.
final class AppSchemeHandler: NSObject, WKURLSchemeHandler {
    static let scheme = "icb"
    static let startURL = URL(string: "icb://localhost/index.html")!

    private let root: URL = Bundle.main.resourceURL!.appendingPathComponent("web", isDirectory: true).standardizedFileURL

    func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
        guard let url = task.request.url else { return fail(task, 400) }
        var path = url.path
        if path.isEmpty || path == "/" { path = "/index.html" }
        let file = root.appendingPathComponent(String(path.dropFirst())).standardizedFileURL
        // nothing outside the web folder
        guard file.path.hasPrefix(root.path + "/"), let data = try? Data(contentsOf: file) else {
            return fail(task, 404)
        }
        let headers = [
            "Content-Type": Self.mimeType(file.pathExtension),
            "Content-Length": String(data.count),
            "Cache-Control": "no-cache",
        ]
        let resp = HTTPURLResponse(url: url, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: headers)!
        task.didReceive(resp)
        task.didReceive(data)
        task.didFinish()
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}

    private func fail(_ task: WKURLSchemeTask, _ code: Int) {
        let resp = HTTPURLResponse(url: task.request.url ?? Self.startURL, statusCode: code, httpVersion: "HTTP/1.1", headerFields: nil)!
        task.didReceive(resp)
        task.didReceive(Data())
        task.didFinish()
    }

    static func mimeType(_ ext: String) -> String {
        switch ext.lowercased() {
        case "html": return "text/html; charset=utf-8"
        case "js", "mjs": return "text/javascript; charset=utf-8"
        case "css": return "text/css; charset=utf-8"
        case "json": return "application/json"
        case "svg": return "image/svg+xml"
        case "wav": return "audio/wav"
        case "woff2": return "font/woff2"
        case "woff": return "font/woff"
        default:
            return UTType(filenameExtension: ext)?.preferredMIMEType ?? "application/octet-stream"
        }
    }
}
