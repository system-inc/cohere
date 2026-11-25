import Foundation

func databaseAddressProbe() -> String {
    let database_url = ProcessInfo.processInfo.environment["DATABASE_URL"] ?? ""
    return database_url
}
