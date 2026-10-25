import Foundation
import CoreServices

let appURL = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let status = LSRegisterURL(appURL as CFURL, true)
if status != noErr {
    fputs("Could not refresh Atlas registration: \(status)\n", stderr)
    exit(1)
}
print("Refreshed Atlas’s macOS registration.")
