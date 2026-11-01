// Small menu bar app that shows whether the OpenMessage background service is running
// and lets you stop, start or restart it. Build and install with ./install.sh.
import AppKit

// To change the menu bar icon, replace these SF Symbol names and run ./install.sh again.
enum Icon {
    static let running = "message.fill"
    static let attention = "exclamationmark.bubble.fill"
    static let stopped = "message"
}

let serviceLabel = "com.cjmcqueen.openmessage"
let webURL = URL(string: "http://127.0.0.1:7007")!
let statusURL = webURL.appendingPathComponent("api/status")
let plistPath = NSHomeDirectory() + "/Library/LaunchAgents/\(serviceLabel).plist"
let logPath = NSHomeDirectory() + "/Library/Logs/OpenMessage/service.log"
let domain = "gui/\(getuid())"
let startupGrace: TimeInterval = 60

enum ServiceState: Equatable {
    case running
    case starting
    case stopped
    case attention(String)
    case unmanaged

    var title: String {
        switch self {
        case .running: return "OpenMessage: Running"
        case .starting: return "OpenMessage: Starting…"
        case .stopped: return "OpenMessage: Stopped"
        case .attention(let reason): return "OpenMessage: \(reason)"
        case .unmanaged: return "OpenMessage: Running outside the service"
        }
    }
}

@discardableResult
func launchctl(_ arguments: String...) -> (ok: Bool, output: String) {
    let process = Process()
    let pipe = Pipe()
    process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
    process.arguments = arguments
    process.standardOutput = pipe
    process.standardError = pipe
    do { try process.run() } catch { return (false, error.localizedDescription) }
    let data = pipe.fileHandleForReading.readDataToEndOfFile()
    process.waitUntilExit()
    return (process.terminationStatus == 0, String(decoding: data, as: UTF8.self))
}

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let stateItem = NSMenuItem(title: "OpenMessage", action: nil, keyEquivalent: "")
    private let startItem = NSMenuItem(title: "Start", action: #selector(start), keyEquivalent: "")
    private let stopItem = NSMenuItem(title: "Stop", action: #selector(stop), keyEquivalent: "")
    private let restartItem = NSMenuItem(title: "Restart", action: #selector(restart), keyEquivalent: "")
    private var state = ServiceState.starting
    private var unreachableSince: Date?
    private var timer: Timer?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let menu = NSMenu()
        menu.autoenablesItems = false
        menu.delegate = self
        stateItem.isEnabled = false
        menu.addItem(stateItem)
        menu.addItem(.separator())
        [startItem, stopItem, restartItem].forEach { item in
            item.target = self
            menu.addItem(item)
        }
        menu.addItem(.separator())
        addItem("Open Inbox", #selector(openInbox), to: menu)
        addItem("Show Log", #selector(showLog), to: menu)
        menu.addItem(.separator())
        addItem("Quit Menu Bar App", #selector(quit), to: menu)
        statusItem.menu = menu

        apply(.starting)
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 10, repeats: true) { [weak self] _ in self?.refresh() }
    }

    func menuWillOpen(_ menu: NSMenu) { refresh() }

    private func addItem(_ title: String, _ action: Selector, to menu: NSMenu) {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
        item.target = self
        menu.addItem(item)
    }

    private func refresh() {
        var request = URLRequest(url: statusURL)
        request.timeoutInterval = 3
        URLSession.shared.dataTask(with: request) { [weak self] data, response, _ in
            let reachable = (response as? HTTPURLResponse)?.statusCode == 200
            let loaded = launchctl("print", "\(domain)/\(serviceLabel)").ok
            DispatchQueue.main.async {
                self?.update(reachable: reachable, loaded: loaded, status: reachable ? data : nil)
            }
        }.resume()
    }

    private func update(reachable: Bool, loaded: Bool, status: Data?) {
        if reachable {
            unreachableSince = nil
            apply(loaded ? healthState(status) : .unmanaged)
        } else if !loaded {
            unreachableSince = nil
            apply(.stopped)
        } else {
            let since = unreachableSince ?? Date()
            unreachableSince = since
            apply(Date().timeIntervalSince(since) < startupGrace ? .starting : .attention("Not responding"))
        }
    }

    private func healthState(_ data: Data?) -> ServiceState {
        guard let data,
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let google = json["google"] as? [String: Any] else { return .attention("Status unknown") }
        if google["needs_pairing"] as? Bool == true { return .attention("Needs pairing") }
        if google["paired"] as? Bool != true { return .attention("Not paired") }
        if google["phone_responding"] as? Bool != true { return .attention("Phone not responding") }
        return .running
    }

    private func apply(_ newState: ServiceState) {
        state = newState
        stateItem.title = newState.title
        startItem.isEnabled = newState == .stopped
        stopItem.isEnabled = newState != .stopped && newState != .unmanaged
        restartItem.isEnabled = stopItem.isEnabled
        statusItem.button?.image = image(for: newState)
        statusItem.button?.appearsDisabled = newState == .stopped
        statusItem.button?.toolTip = newState.title
    }

    private func image(for state: ServiceState) -> NSImage? {
        let name: String
        switch state {
        case .running: name = Icon.running
        case .stopped: name = Icon.stopped
        case .starting, .attention, .unmanaged: name = Icon.attention
        }
        var image = NSImage(systemSymbolName: name, accessibilityDescription: state.title)
        if case .attention = state {
            let orange = NSImage.SymbolConfiguration(paletteColors: [.systemOrange])
            image = image?.withSymbolConfiguration(orange)
            image?.isTemplate = false
        } else {
            image?.isTemplate = true
        }
        return image
    }

    // Stop lasts until the next login, when the service starts on its own again.
    private func runService(_ arguments: [String], failure: String) {
        DispatchQueue.global().async { [weak self] in
            let process = arguments.count == 2
                ? launchctl(arguments[0], arguments[1])
                : launchctl(arguments[0], arguments[1], arguments[2])
            DispatchQueue.main.async {
                if !process.ok { self?.showError(failure, process.output) }
                self?.refresh()
            }
        }
    }

    private func showError(_ message: String, _ detail: String) {
        let alert = NSAlert()
        alert.messageText = message
        alert.informativeText = detail
        NSApp.activate(ignoringOtherApps: true)
        alert.runModal()
    }

    @objc private func start() {
        apply(.starting)
        runService(["bootstrap", domain, plistPath], failure: "OpenMessage didn't start")
    }

    @objc private func stop() {
        runService(["bootout", "\(domain)/\(serviceLabel)"], failure: "OpenMessage didn't stop")
    }

    @objc private func restart() {
        apply(.starting)
        runService(["kickstart", "-k", "\(domain)/\(serviceLabel)"], failure: "OpenMessage didn't restart")
    }

    @objc private func openInbox() { NSWorkspace.shared.open(webURL) }

    @objc private func showLog() { NSWorkspace.shared.open(URL(fileURLWithPath: logPath)) }

    @objc private func quit() { NSApp.terminate(nil) }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
app.run()
