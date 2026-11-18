import UIKit
import WebKit
import UniformTypeIdentifiers
import IrcCore

/// Hosts the shared frontend in a WKWebView and handles the messages bridge.js sends: calls
/// into the Go core, opening links, system bars, backups and sharing the app log. The Swift
/// twin of android/…/MainActivity.kt.
final class WebViewController: UIViewController, WKScriptMessageHandler, WKNavigationDelegate,
    WKUIDelegate, UIScrollViewDelegate, UIDocumentPickerDelegate {

    private var webView: WKWebView!
    private var statusStyle: UIStatusBarStyle = .lightContent
    private enum Picking { case save, open }
    private var picking: Picking?
    private var exportFile: URL?

    override var preferredStatusBarStyle: UIStatusBarStyle { statusStyle }

    override func loadView() {
        let config = WKWebViewConfiguration()
        config.setURLSchemeHandler(AppSchemeHandler(), forURLScheme: AppSchemeHandler.scheme)
        config.allowsInlineMediaPlayback = true
        config.mediaTypesRequiringUserActionForPlayback = .all
        config.dataDetectorTypes = []

        let ucc = WKUserContentController()
        if let src = Bundle.main.url(forResource: "bridge", withExtension: "js"),
           let js = try? String(contentsOf: src, encoding: .utf8) {
            ucc.addUserScript(WKUserScript(source: js, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        }
        // no zooming in when a text field is focused (iOS does that for fonts under 16px)
        ucc.addUserScript(WKUserScript(source: """
            (function(){var m=document.querySelector('meta[name=viewport]');
            if(m)m.setAttribute('content','width=device-width, initial-scale=1.0, maximum-scale=1.0');
            document.body&&document.body.classList.add('ios');})();
            """, injectionTime: .atDocumentEnd, forMainFrameOnly: true))
        // the handler only answers the app's own page (checked again in userContentController)
        ucc.add(WeakHandler(self), contentWorld: .page, name: "icb")
        config.userContentController = ucc

        let root = UIView()
        root.backgroundColor = UIColor(named: "LaunchBackground")
        webView = WKWebView(frame: .zero, configuration: config)
        webView.translatesAutoresizingMaskIntoConstraints = false
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        webView.scrollView.contentInsetAdjustmentBehavior = .never
        webView.scrollView.bounces = false
        webView.scrollView.delegate = self
        webView.allowsLinkPreview = false
        webView.allowsBackForwardNavigationGestures = false
        webView.navigationDelegate = self
        webView.uiDelegate = self
        #if DEBUG
        if #available(iOS 16.4, *) { webView.isInspectable = true }
        #endif
        root.addSubview(webView)
        // inside the safe area (notch, home indicator), and above the keyboard: the page
        // shrinks when the keyboard opens, like Android's adjustResize
        root.keyboardLayoutGuide.followsUndockedKeyboard = true
        NSLayoutConstraint.activate([
            webView.topAnchor.constraint(equalTo: root.safeAreaLayoutGuide.topAnchor),
            webView.leadingAnchor.constraint(equalTo: root.safeAreaLayoutGuide.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: root.safeAreaLayoutGuide.trailingAnchor),
            webView.bottomAnchor.constraint(equalTo: root.keyboardLayoutGuide.topAnchor),
        ])
        view = root
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        Core.shared.page = self
        webView.load(URLRequest(url: AppSchemeHandler.startURL))
    }

    /// Runs JavaScript in the page (main thread).
    func run(_ js: String) {
        webView?.evaluateJavaScript(js, completionHandler: nil)
    }

    // The page itself never scrolls (the UI scrolls inside); undo iOS nudging it when a
    // field is focused.
    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        if scrollView.contentOffset != .zero { scrollView.contentOffset = .zero }
    }

    // MARK: messages from bridge.js

    func userContentController(_ ucc: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.frameInfo.isMainFrame,
              message.frameInfo.securityOrigin.protocol == AppSchemeHandler.scheme,
              let text = message.body as? String,
              let data = text.data(using: .utf8),
              let msg = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let type = msg["t"] as? String else { return }

        switch type {
        case "call":
            guard let id = msg["id"] as? Int, let m = msg["m"] as? String else { return }
            Core.shared.call(id: id, method: m, args: msg["a"] as? String ?? "[]")
        case "ready":
            Core.shared.uiReady()
        case "openUrl":
            Core.shared.openURL(msg["url"] as? String)
        case "bars":
            setBars(color: msg["color"] as? String ?? "", light: msg["light"] as? Bool ?? false)
        case "notifications":
            Core.shared.requestNotifications()
        case "notificationSettings":
            if let u = URL(string: UIApplication.openNotificationSettingsURLString) { UIApplication.shared.open(u) }
        case "vibrate":
            UIImpactFeedbackGenerator(style: .light).impactOccurred()
        case "saveBackup":
            saveBackup(name: msg["name"] as? String ?? "irchatterbox-settings.json", data: msg["data"] as? String ?? "")
        case "openBackup":
            openBackup()
        case "shareLogs":
            shareLogs()
        default:
            break
        }
    }

    private func backupDone(_ result: [String: Any]) {
        Core.shared.emitToPage("androidBackup", result)
    }

    // MARK: system bars

    private func setBars(color: String, light: Bool) {
        if let c = UIColor(css: color) {
            view.backgroundColor = c
        }
        statusStyle = light ? .darkContent : .lightContent
        setNeedsStatusBarAppearanceUpdate()
    }

    // MARK: settings backup

    private func saveBackup(name: String, data: String) {
        let safe = name.replacingOccurrences(of: "/", with: "_")
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(safe.isEmpty ? "irchatterbox-settings.json" : safe)
        do {
            try data.write(to: url, atomically: true, encoding: .utf8)
        } catch {
            return backupDone(["error": "Couldn't save the backup: \(error.localizedDescription)"])
        }
        exportFile = url
        picking = .save
        let picker = UIDocumentPickerViewController(forExporting: [url], asCopy: true)
        picker.delegate = self
        present(picker, animated: true)
    }

    private func openBackup() {
        picking = .open
        let picker = UIDocumentPickerViewController(forOpeningContentTypes: [.json, .plainText, .data], asCopy: true)
        picker.delegate = self
        picker.allowsMultipleSelection = false
        present(picker, animated: true)
    }

    func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        let what = picking
        picking = nil
        cleanupExport()
        switch what {
        case .save:
            backupDone([:])
        case .open:
            guard let url = urls.first else { return backupDone(["canceled": true]) }
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }
            guard let d = try? Data(contentsOf: url), d.count < 20 << 20, let s = String(data: d, encoding: .utf8) else {
                return backupDone(["error": "That file couldn't be read."])
            }
            backupDone(["data": s])
        case nil:
            break
        }
    }

    func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
        picking = nil
        cleanupExport()
        backupDone(["canceled": true])
    }

    private func cleanupExport() {
        if let f = exportFile { try? FileManager.default.removeItem(at: f) }
        exportFile = nil
    }

    // MARK: share app log

    private func shareLogs() {
        let dir = (MobileLogDir() as String?) ?? ""
        let log = URL(fileURLWithPath: dir).appendingPathComponent("app.log")
        guard !dir.isEmpty, FileManager.default.fileExists(atPath: log.path) else {
            return backupDone(["error": "There's no app log yet."])
        }
        let sheet = UIActivityViewController(activityItems: [log], applicationActivities: nil)
        sheet.completionWithItemsHandler = { [weak self] _, _, _, _ in self?.backupDone([:]) }
        if let pop = sheet.popoverPresentationController { // iPad
            pop.sourceView = view
            pop.sourceRect = CGRect(x: view.bounds.midX, y: view.bounds.midY, width: 1, height: 1)
            pop.permittedArrowDirections = []
        }
        present(sheet, animated: true)
    }

    // MARK: navigation

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url, let scheme = url.scheme?.lowercased() else {
            return decisionHandler(.cancel)
        }
        if scheme == AppSchemeHandler.scheme || scheme == "about" || scheme == "blob" || scheme == "data" {
            return decisionHandler(.allow)
        }
        let mainFrame = action.targetFrame?.isMainFrame ?? true
        if !mainFrame && (scheme == "https" || scheme == "http") {
            return decisionHandler(.allow) // embedded players (YouTube, Vimeo, …)
        }
        // a link that would replace the app: open it in the browser instead
        if ["http", "https", "mailto"].contains(scheme) && action.navigationType == .linkActivated {
            UIApplication.shared.open(url)
        }
        decisionHandler(.cancel)
    }

    // target=_blank and window.open: the browser
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = action.request.url, ["http", "https", "mailto"].contains(url.scheme?.lowercased() ?? "") {
            UIApplication.shared.open(url)
        }
        return nil
    }

    // The UI registers its event listeners during boot, right after the page loads; give it
    // a moment, then deliver anything waiting (a notification tapped to launch the app).
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { Core.shared.uiReady() }
    }

    // the page was killed while the app was in the background: load it again
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        webView.load(URLRequest(url: AppSchemeHandler.startURL))
    }
}

/// WKUserContentController keeps its handlers alive; this breaks the cycle with the controller.
private final class WeakHandler: NSObject, WKScriptMessageHandler {
    weak var target: WKScriptMessageHandler?
    init(_ target: WKScriptMessageHandler) { self.target = target }
    func userContentController(_ ucc: WKUserContentController, didReceive message: WKScriptMessage) {
        target?.userContentController(ucc, didReceive: message)
    }
}

extension UIColor {
    /// A CSS colour as the theme gives it: #rgb, #rrggbb or rgb()/rgba().
    convenience init?(css: String) {
        let s = css.trimmingCharacters(in: .whitespaces).lowercased()
        var r = 0.0, g = 0.0, b = 0.0
        if s.hasPrefix("#") {
            var hex = String(s.dropFirst())
            if hex.count == 3 { hex = hex.map { "\($0)\($0)" }.joined() }
            guard hex.count == 6, let v = Int(hex, radix: 16) else { return nil }
            r = Double((v >> 16) & 0xff); g = Double((v >> 8) & 0xff); b = Double(v & 0xff)
        } else if s.hasPrefix("rgb") {
            let nums = s.drop(while: { $0 != "(" }).dropFirst().prefix(while: { $0 != ")" })
                .split(separator: ",").compactMap { Double($0.trimmingCharacters(in: .whitespaces)) }
            guard nums.count >= 3 else { return nil }
            r = nums[0]; g = nums[1]; b = nums[2]
        } else {
            return nil
        }
        self.init(red: r / 255, green: g / 255, blue: b / 255, alpha: 1)
    }
}
