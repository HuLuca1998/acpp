import os from "node:os"

/**
 * 取局域网 IPv4 地址，用于「复制局域网链接」。
 * 优先 en0（Wi-Fi/内建网口），其次其他 en*，排除回环与 169.254 链路本地地址。
 */
export function primaryIPv4() {
  const candidates = []
  for (const [name, addrs] of Object.entries(os.networkInterfaces())) {
    for (const addr of addrs ?? []) {
      // Node 18 起 family 是数字 4，更早是字符串 "IPv4"——两种都认，省得
      // 将来换 Node 版本时这里悄悄失灵。
      const isV4 = addr.family === "IPv4" || addr.family === 4
      if (!isV4 || addr.internal) continue
      if (addr.address.startsWith("169.254.")) continue
      candidates.push({ name, ip: addr.address })
    }
  }
  return (
    candidates.find((c) => c.name === "en0")?.ip ??
    candidates.find((c) => c.name.startsWith("en"))?.ip ??
    candidates[0]?.ip ??
    null
  )
}
