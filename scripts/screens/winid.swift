import CoreGraphics
import Foundation
// Prints the window number of the on-screen window whose title contains argv[1].
let want = CommandLine.arguments[1]
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as? [[String: Any]] ?? []
for w in list {
    let owner = w[kCGWindowOwnerName as String] as? String ?? ""
    let name = w[kCGWindowName as String] as? String ?? ""
    if owner.lowercased().contains("ghostty") && name.contains(want) {
        print(w[kCGWindowNumber as String] as! Int)
        exit(0)
    }
}
exit(1)
