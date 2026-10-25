import Cocoa
import WebKit

func atlasArgument(_ name: String) -> String? {
    let arguments = CommandLine.arguments
    guard let index = arguments.firstIndex(of: name), index + 1 < arguments.count else { return nil }
    return arguments[index + 1]
}

func atlasLibraryURL() throws -> URL {
    let configured = atlasArgument("--library") ?? ProcessInfo.processInfo.environment["ATLAS_LIBRARY"]
    if CommandLine.arguments.contains("--library") && atlasArgument("--library") == nil {
        throw NSError(domain: "Atlas", code: 1, userInfo: [NSLocalizedDescriptionKey: "--library requires an absolute path."])
    }
    if let path = configured {
        guard path.hasPrefix("/") else {
            throw NSError(domain: "Atlas", code: 1, userInfo: [NSLocalizedDescriptionKey: "Atlas library must be an absolute path."])
        }
        return URL(fileURLWithPath: path, isDirectory: true).standardizedFileURL.resolvingSymlinksInPath()
    }
    return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Documents/Atlas/library", isDirectory: true).standardizedFileURL.resolvingSymlinksInPath()
}

final class AtlasDelegate: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKUIDelegate {
    var window: NSWindow!
    var web: WKWebView!
    var origin: URL?
    var libraryURL: URL!

    func applicationDidFinishLaunching(_ notification: Notification) {
        do { libraryURL = try atlasLibraryURL() }
        catch { fail(error.localizedDescription); NSApp.terminate(nil); return }
        if let iconURL = Bundle.main.url(forResource: "Atlas", withExtension: "icns"),
           let icon = NSImage(contentsOf: iconURL) {
            NSApplication.shared.applicationIconImage = icon
        }
        let menu = NSMenu()
        let appItem = NSMenuItem(); menu.addItem(appItem)
        let appMenu = NSMenu(); appItem.submenu = appMenu
        appMenu.addItem(withTitle: "About Atlas", action: #selector(about), keyEquivalent: "")
        appMenu.addItem(withTitle: "Show Atlas Library", action: #selector(showLibrary), keyEquivalent: "l")
        appMenu.addItem(NSMenuItem.separator())
        appMenu.addItem(withTitle: "Quit Atlas", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        let editItem = NSMenuItem(); menu.addItem(editItem)
        let edit = NSMenu(title: "Edit"); editItem.submenu = edit
        edit.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        edit.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        edit.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        edit.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        edit.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        NSApp.mainMenu = menu
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1350, height: 880), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        window.title = "Atlas"
        window.minSize = NSSize(width: 980, height: 650)
        window.center()
        let config = WKWebViewConfiguration()
        web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = self
        web.uiDelegate = self
        window.contentView = web
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        connectToService()
    }
    func connectToService(attempt: Int = 0) {
        let settingsURL = libraryURL.appendingPathComponent(".service/connection.json")
        guard let data = try? Data(contentsOf: settingsURL),
              let settings = try? JSONSerialization.jsonObject(with: data) as? [String: String],
              let address = settings["url"], let token = settings["token"],
              let base = URL(string: address), base.scheme == "http", base.host == "127.0.0.1",
              settings["library"] == libraryURL.path else {
            fail("Atlas’s shared service is not configured. Run scripts/service.py install in the Atlas project.")
            return
        }
        var request = URLRequest(url: base.appendingPathComponent("health"))
        request.setValue("Bearer " + token, forHTTPHeaderField: "Authorization")
        request.timeoutInterval = 2
        URLSession.shared.dataTask(with: request) { [weak self] data, response, error in
            DispatchQueue.main.async {
                guard let self = self else { return }
                if let response = response as? HTTPURLResponse, response.statusCode == 200,
                   let data = data, let health = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                   health["service"] as? String == "atlas", health["library"] as? String == self.libraryURL.path {
                    var components = URLComponents(url: base, resolvingAgainstBaseURL: false)!
                    components.path = "/"
                    components.queryItems = [URLQueryItem(name: "token", value: token)]
                    guard let url = components.url else { return }
                    self.origin = base
                    self.web.load(URLRequest(url: url))
                } else if attempt < 10 {
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { self.connectToService(attempt: attempt + 1) }
                } else {
                    self.fail("Atlas’s shared service is unavailable. Run scripts/service.py start or restart in the Atlas project.")
                }
            }
        }.resume()
    }
    func fail(_ message: String) {
        let alert = NSAlert(); alert.messageText = "Atlas could not start"; alert.informativeText = message; alert.runModal()
    }
    @objc func about() {
        let alert = NSAlert(); alert.messageText = "Atlas"; alert.informativeText = "A map of your work.\nLocal prototype · version 0.1\nNotes, sources, and organization stay on this Mac."; alert.runModal()
    }
    @objc func showLibrary() { NSWorkspace.shared.open(libraryURL) }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = navigationAction.request.url else { decisionHandler(.cancel); return }
        if url.host == origin?.host && url.port == origin?.port { decisionHandler(.allow) }
        else { if ["https", "http"].contains(url.scheme ?? "") { NSWorkspace.shared.open(url) }; decisionHandler(.cancel) }
    }
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = navigationAction.request.url, ["https", "http"].contains(url.scheme ?? "") { NSWorkspace.shared.open(url) }; return nil
    }
}
// The launch agent uses the signed Atlas app as its responsible code so macOS
// can attribute Documents-folder consent to Atlas rather than an anonymous backend.
final class AtlasServiceDelegate: NSObject, NSApplicationDelegate {
    var process: Process?
    var terminationSignal: DispatchSourceSignal?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let arguments = CommandLine.arguments
        func value(_ key: String) -> String? {
            guard let index = arguments.firstIndex(of: key), index + 1 < arguments.count else { return nil }
            return arguments[index + 1]
        }
        guard let library = value("--library") else {
            fputs("Atlas service library argument is missing.\n", stderr); exit(1)
        }
        let requestAccess = arguments.contains("--request-access")
        if requestAccess { NSApp.activate(ignoringOtherApps: true) }
        let goService = value("--go-service")
        var executable = ""
        var childArguments: [String] = []
        if !requestAccess {
            guard library.hasPrefix("/"), let port = value("--port"), let number = Int(port), (0...65535).contains(number) else {
                fputs("Atlas service requires an absolute library and a valid port.\n", stderr); exit(1)
            }
            guard let go = goService, !arguments.contains("--python"), !arguments.contains("--server"), go.hasPrefix("/"),
                  let owner = value("--owner-name"), !owner.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                  let timezone = value("--timezone"), !timezone.isEmpty else {
                fputs("Atlas requires an absolute Go executable, owner-name and timezone.\n", stderr); exit(1)
            }
            executable = go
            childArguments = [arguments.contains("--contract-mode") ? "--contract-mode" : "--serve",
                              "--library", library, "--port", port, "--owner-name", owner, "--timezone", timezone]
        }
        // This ordinary file access lets macOS present and remember consent for
        // the Atlas app. Do not suppress, reset or bypass privacy decisions.
        do {
            _ = try FileManager.default.contentsOfDirectory(atPath: library)
        } catch {
            fputs("Atlas Service needs Documents access in System Settings > Privacy & Security > Files & Folders.\n", stderr)
            if requestAccess {
                let alert = NSAlert()
                alert.messageText = "Atlas Service needs access to Documents"
                alert.informativeText = "Allow Documents Folder access for Atlas Service in System Settings > Privacy & Security > Files & Folders, then try again."
                alert.runModal()
            }
            exit(1)
        }
        if requestAccess { exit(0) }
        let child = Process()
        child.executableURL = URL(fileURLWithPath: executable)
        child.arguments = childArguments
        child.standardOutput = FileHandle.standardOutput
        child.standardError = FileHandle.standardError
        child.terminationHandler = { child in exit(child.terminationStatus) }
        signal(SIGTERM, SIG_IGN)
        let source = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
        source.setEventHandler { if child.isRunning { child.terminate() } else { exit(0) } }
        source.resume(); terminationSignal = source
        do { try child.run(); process = child }
        catch { fputs("Could not launch Atlas service: \(error.localizedDescription)\n", stderr); exit(1) }
    }
}

let app = NSApplication.shared
let serviceMode = CommandLine.arguments.contains("--service") || CommandLine.arguments.contains("--request-access")
let delegate: NSApplicationDelegate = serviceMode ? AtlasServiceDelegate() : AtlasDelegate()
app.delegate = delegate
app.setActivationPolicy(serviceMode ? .accessory : .regular)
app.run()
