import UIKit
import UserNotifications

@main
final class AppDelegate: UIResponder, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        Core.shared.start()
        return true
    }

    func application(_ application: UIApplication,
                     configurationForConnecting connectingSceneSession: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        UISceneConfiguration(name: "Default", sessionRole: connectingSceneSession.role)
    }

    // A notification arriving while the app is open: show it as a banner anyway (the core
    // only notifies for chats you aren't looking at).
    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        var opts: UNNotificationPresentationOptions = [.banner, .list]
        if notification.request.content.sound != nil { opts.insert(.sound) }
        completionHandler(opts)
    }

    // Tapping a notification opens that chat.
    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                didReceive response: UNNotificationResponse,
                                withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let server = info["server"] as? String ?? ""
        let buffer = info["buffer"] as? String ?? ""
        if !server.isEmpty {
            DispatchQueue.main.async { Core.shared.focus(server: server, buffer: buffer) }
        }
        completionHandler()
    }
}
