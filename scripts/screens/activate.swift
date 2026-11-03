import AppKit
// Brings the app with pid argv[1] to the front.
guard CommandLine.arguments.count > 1, let pid = pid_t(CommandLine.arguments[1]) else { exit(2) }
if let app = NSRunningApplication(processIdentifier: pid) {
    print(app.activate(options: [.activateAllWindows]))
}
