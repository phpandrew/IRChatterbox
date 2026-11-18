import Foundation
import UIKit
import UserNotifications
import IrcCore

/// Owns the Go core (IrcCore.xcframework, built from source/mobile) and is its Host: events
/// from Go go to the WebView, notifications to Notification Center. The Swift twin of
/// android/…/CoreHost.kt.
final class Core: NSObject, MobileHostProtocol {
    static let shared = Core()

    /// The page currently showing the UI (set by WebViewController).
    weak var page: WebViewController?

    private var started = false
    private var ready = false              // the UI said its listeners are registered
    private var pendingFocus: (String, String)?

    private let lock = NSLock()
    private var queued: [String] = []      // "[event,data]" pairs waiting for the next flush
    private var flushScheduled = false

    func start() {
        guard !started else { return }
        started = true
        let fm = FileManager.default
        let base = fm.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("IRChatterbox", isDirectory: true)
        try? fm.createDirectory(at: base, withIntermediateDirectories: true)
        // config, logs and caches; backed up with the phone like any app data
        let version = Bundle.main.object(forInfoDictionaryKey: "ICBFullVersion") as? String ?? "dev"
        MobileSetStore("appstore")  // TestFlight / the App Store deliver updates
        MobileStart(base.path, version, self)
    }

    // MARK: calls from the UI

    /// Runs an App method off the main thread and answers the page's promise.
    func call(id: Int, method: String, args: String) {
        let stuck = DispatchWorkItem { MobileReportStuck(method, 30) }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 30, execute: stuck)
        DispatchQueue.global(qos: .userInitiated).async {
            let reply = (MobileCall(method, args) as String?) ?? "null"
            stuck.cancel()
            DispatchQueue.main.async {
                self.page?.run("window.__icb&&window.__icb.resolve(\(id),\(reply))")
            }
        }
    }

    func uiReady() {
        let first = !ready
        ready = true
        if let p = pendingFocus {
            pendingFocus = nil
            focus(server: p.0, buffer: p.1)
        }
        refreshStatus()
        guard first else { return }
        // first launch: ask once whether notifications are allowed
        UNUserNotificationCenter.current().getNotificationSettings { s in
            if s.authorizationStatus == .notDetermined { self.requestNotifications() }
        }
    }

    /// Opens a chat (a tapped notification). Waits for the UI if it isn't up yet.
    func focus(server: String, buffer: String) {
        guard ready, page != nil else { pendingFocus = (server, buffer); return }
        let data = Core.json(["server": server, "buffer": buffer])
        page?.run("window.__icb&&window.__icb.emit([[\"focusBuffer\",\(data)]])")
    }

    /// Delivers an event to the page as if the core had emitted it.
    func emitToPage(_ event: String, _ data: [String: Any]) {
        emit(event, dataJSON: Core.json(data))
    }

    func requestNotifications() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in
            self.refreshStatus()
        }
    }

    /// Tells the settings page whether notifications are allowed.
    func refreshStatus() {
        UNUserNotificationCenter.current().getNotificationSettings { s in
            let ok = s.authorizationStatus == .authorized || s.authorizationStatus == .provisional
            DispatchQueue.main.async {
                self.page?.run("window.__icb&&window.__icb.status({notifications:\(ok)})")
            }
        }
    }

    // MARK: MobileHost (called by Go, on any thread)

    func emit(_ event: String?, dataJSON: String?) {
        guard let event = event else { return }
        let data = (dataJSON?.isEmpty ?? true) ? "null" : dataJSON!
        lock.lock()
        queued.append("[\(Core.json(event)),\(data)]")
        let schedule = !flushScheduled
        flushScheduled = true
        lock.unlock()
        // batch events that arrive together (a burst of history) into one call into the page
        if schedule {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.016) { self.flush() }
        }
    }

    private func flush() {
        lock.lock()
        let batch = queued
        queued.removeAll()
        flushScheduled = false
        lock.unlock()
        guard !batch.isEmpty else { return }
        page?.run("window.__icb&&window.__icb.emit([\(batch.joined(separator: ","))])")
    }

    func showNotification(_ title: String?, body: String?, server: String?, buffer: String?, silent: Bool) {
        let c = UNMutableNotificationContent()
        c.title = title ?? ""
        c.body = body ?? ""
        c.userInfo = ["server": server ?? "", "buffer": buffer ?? ""]
        c.threadIdentifier = (server ?? "") + "\u{1}" + (buffer ?? "")  // stack per chat
        if !silent { c.sound = .default }
        let req = UNNotificationRequest(identifier: UUID().uuidString, content: c, trigger: nil)
        UNUserNotificationCenter.current().add(req)
    }

    func openURL(_ url: String?) {
        guard let s = url, let u = URL(string: s), ["http", "https"].contains(u.scheme?.lowercased() ?? "") else { return }
        DispatchQueue.main.async { UIApplication.shared.open(u) }
    }

    func connected(_ n: Int) {}

    func installAPK(_ path: String?) -> String {
        "Updates are installed by TestFlight or the App Store."
    }

    // MARK: helpers

    /// JSON text for a string or dictionary, safe to paste into JavaScript.
    static func json(_ value: Any) -> String {
        if let s = value as? String {
            if let d = try? JSONSerialization.data(withJSONObject: [s]), var t = String(data: d, encoding: .utf8) {
                t.removeFirst(); t.removeLast()
                return t.replacingOccurrences(of: "\u{2028}", with: "\\u2028").replacingOccurrences(of: "\u{2029}", with: "\\u2029")
            }
            return "\"\""
        }
        guard JSONSerialization.isValidJSONObject(value),
              let d = try? JSONSerialization.data(withJSONObject: value),
              let t = String(data: d, encoding: .utf8) else { return "null" }
        return t.replacingOccurrences(of: "\u{2028}", with: "\\u2028").replacingOccurrences(of: "\u{2029}", with: "\\u2029")
    }
}
