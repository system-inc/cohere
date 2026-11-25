import Foundation

func databaseAddressProbe() -> String {
    let DATABASE_URL = ProcessInfo.processInfo.environment["DATABASE_URL"] ?? ""
    return DATABASE_URL
}
