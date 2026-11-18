import UIKit
import IrcCore

final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
    var window: UIWindow?

    // iOS suspends apps shortly after they leave the screen, which silently kills their
    // connections. So, like Android with "Keep connection live" off, the app disconnects
    // cleanly in the background and reconnects when you come back; a bouncer replays what
    // you missed. The disconnect waits ~20 seconds (inside the time iOS allows) so a quick
    // trip to another app doesn't drop anything.
    private var bgTask: UIBackgroundTaskIdentifier = .invalid
    private var parkWork: DispatchWorkItem?
    private var parked = false

    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions) {
        guard let windowScene = scene as? UIWindowScene else { return }
        let window = UIWindow(windowScene: windowScene)
        window.rootViewController = WebViewController()
        window.makeKeyAndVisible()
        self.window = window
        // launched by tapping a notification
        if let resp = connectionOptions.notificationResponse {
            let info = resp.notification.request.content.userInfo
            if let server = info["server"] as? String, !server.isEmpty {
                Core.shared.focus(server: server, buffer: info["buffer"] as? String ?? "")
            }
        }
    }

    func sceneDidEnterBackground(_ scene: UIScene) {
        parkWork?.cancel()
        let app = UIApplication.shared
        bgTask = app.beginBackgroundTask(withName: "disconnect") { [weak self] in
            // out of time: start the disconnect and hand the time back
            self?.park()
            self?.endTask()
        }
        let work = DispatchWorkItem { [weak self] in self?.park() }
        parkWork = work
        DispatchQueue.main.asyncAfter(deadline: .now() + 20, execute: work)
    }

    func sceneWillEnterForeground(_ scene: UIScene) {
        parkWork?.cancel()
        parkWork = nil
        endTask()
        if parked {
            parked = false
            DispatchQueue.global(qos: .userInitiated).async { MobileSetBackground(false) }
        }
        Core.shared.refreshStatus()
    }

    private func park() {
        guard !parked else { endTask(); return }
        parked = true
        parkWork = nil
        DispatchQueue.global(qos: .userInitiated).async {
            MobileSetBackground(true)
            DispatchQueue.main.async { self.endTask() }
        }
    }

    private func endTask() {
        if bgTask != .invalid {
            UIApplication.shared.endBackgroundTask(bgTask)
            bgTask = .invalid
        }
    }
}
