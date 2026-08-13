import Foundation

/// 取局域网 IPv4 地址，用于「复制局域网链接」。
/// 优先 en0（Wi-Fi/内建网口），其次其他 en*，排除回环与 169.254 链路本地地址。
enum LanAddress {
    static func primaryIPv4() -> String? {
        var addrs: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&addrs) == 0, let first = addrs else { return nil }
        defer { freeifaddrs(addrs) }

        var candidates: [(name: String, ip: String)] = []
        var cursor: UnsafeMutablePointer<ifaddrs>? = first
        while let ifa = cursor {
            defer { cursor = ifa.pointee.ifa_next }
            guard let sa = ifa.pointee.ifa_addr, sa.pointee.sa_family == UInt8(AF_INET) else { continue }
            let flags = Int32(bitPattern: ifa.pointee.ifa_flags)
            guard flags & IFF_UP != 0, flags & IFF_LOOPBACK == 0 else { continue }
            var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            guard getnameinfo(sa, socklen_t(sa.pointee.sa_len), &host, socklen_t(host.count), nil, 0, NI_NUMERICHOST) == 0 else { continue }
            let ip = String(cString: host)
            guard !ip.hasPrefix("169.254.") else { continue }
            candidates.append((String(cString: ifa.pointee.ifa_name), ip))
        }

        if let hit = candidates.first(where: { $0.name == "en0" }) { return hit.ip }
        if let hit = candidates.first(where: { $0.name.hasPrefix("en") }) { return hit.ip }
        return candidates.first?.ip
    }
}
