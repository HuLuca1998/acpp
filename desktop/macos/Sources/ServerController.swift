import AppKit

/// 管理捆绑的 acp-server 子进程：端口占用清理、启动、健康等待、局域网开关、优雅停止。
final class ServerController {
    /// 桌面版固定端口。与开发态 48080 隔离：dev.sh 清理 48080 时不会误杀桌面版，
    /// 桌面版清理 48090 时也不会碰开发进程（端口策略沿用仓库惯例，见 ADR-004）。
    static let port = 48090

    enum State { case stopped, starting, running, failed }
    private(set) var state: State = .stopped

    var localURL: URL { URL(string: "http://127.0.0.1:\(Self.port)/")! }

    var lanShareEnabled: Bool {
        get { UserDefaults.standard.bool(forKey: "LanShareEnabled") }
        set { UserDefaults.standard.set(newValue, forKey: "LanShareEnabled") }
    }

    var lanURL: String? {
        guard let ip = LanAddress.primaryIPv4() else { return nil }
        return "http://\(ip):\(Self.port)/"
    }

    let logPath = ("~/Library/Logs/ACPP/server.log" as NSString).expandingTildeInPath

    private var process: Process?
    /// stop() 主动停时置真，terminationHandler 据此区分「意外退出」。
    private var deliberateStop = false

    // MARK: - 生命周期

    /// 启动流程：清掉 48090 上的遗留进程（上次强杀留下的孤儿）→ 拉起子进程 → 等健康检查。
    func start(completion: @escaping (Bool) -> Void) {
        state = .starting
        killPortOccupant()
        guard spawn() else {
            state = .failed
            completion(false)
            return
        }
        waitHealthy { [weak self] ok in
            self?.state = ok ? .running : .failed
            completion(ok)
        }
    }

    func restart(completion: @escaping (Bool) -> Void) {
        stop()
        start(completion: completion)
    }

    /// 优雅停止：SIGTERM 给后端 10 秒收尾逻辑一个触发点，3 秒等不到就 SIGKILL。
    /// 只在退出/重启路径调用，短暂阻塞主线程可接受。
    func stop() {
        deliberateStop = true
        guard let p = process, p.isRunning else {
            process = nil
            state = .stopped
            return
        }
        p.terminate()
        let deadline = Date().addingTimeInterval(3)
        while p.isRunning && Date() < deadline {
            usleep(100_000)
        }
        if p.isRunning {
            kill(p.processIdentifier, SIGKILL)
        }
        process = nil
        state = .stopped
    }

    // MARK: - 子进程

    private func spawn() -> Bool {
        deliberateStop = false
        let bundleURL = Bundle.main.bundleURL
        let serverURL = bundleURL.appendingPathComponent("Contents/MacOS/acp-server")
        let webDir = bundleURL.appendingPathComponent("Contents/Resources/web").path

        let p = Process()
        p.executableURL = serverURL

        var env = ProcessInfo.processInfo.environment
        let host = lanShareEnabled ? "0.0.0.0" : "127.0.0.1"
        env["ACP_ADDR"] = "\(host):\(Self.port)"
        env["ACP_WEB_DIR"] = webDir
        // GUI 启动的 app 只有系统级 PATH，server 靠 PATH 拉起 claude/codex 等
        // agent 子进程——必须注入登录 shell 的 PATH，否则核心功能直接瘫痪。
        env["PATH"] = Self.loginShellPATH
        p.environment = env

        if let log = openLog() {
            p.standardOutput = log
            p.standardError = log
        }

        p.terminationHandler = { [weak self] _ in
            DispatchQueue.main.async {
                guard let self, !self.deliberateStop else { return }
                self.state = .failed
                self.process = nil
            }
        }

        do {
            try p.run()
        } catch {
            NSLog("acp-server 启动失败: \(error)")
            return false
        }
        process = p
        return true
    }

    private func openLog() -> FileHandle? {
        let fm = FileManager.default
        let dir = (logPath as NSString).deletingLastPathComponent
        try? fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
        if !fm.fileExists(atPath: logPath) {
            fm.createFile(atPath: logPath, contents: nil)
        }
        let handle = FileHandle(forWritingAtPath: logPath)
        handle?.seekToEndOfFile()
        return handle
    }

    /// 清理 48090 上的遗留监听进程。app 被 SIGKILL 时来不及回收子进程，
    /// 下次启动按仓库端口策略处理：占口者一律清掉后在原端口重启。
    private func killPortOccupant() {
        let pids = shellLines("/usr/sbin/lsof", ["-ti", "tcp:\(Self.port)", "-sTCP:LISTEN"])
        guard !pids.isEmpty else { return }
        for pid in pids.compactMap({ Int32($0) }) {
            kill(pid, SIGTERM)
        }
        let deadline = Date().addingTimeInterval(2)
        while Date() < deadline {
            if shellLines("/usr/sbin/lsof", ["-ti", "tcp:\(Self.port)", "-sTCP:LISTEN"]).isEmpty { return }
            usleep(100_000)
        }
        for pid in shellLines("/usr/sbin/lsof", ["-ti", "tcp:\(Self.port)", "-sTCP:LISTEN"]).compactMap({ Int32($0) }) {
            kill(pid, SIGKILL)
        }
    }

    // MARK: - 健康检查

    func healthOK() -> Bool {
        var ok = false
        let sem = DispatchSemaphore(value: 0)
        var req = URLRequest(url: localURL.appendingPathComponent("api/health"))
        req.timeoutInterval = 0.8
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            ok = (resp as? HTTPURLResponse)?.statusCode == 200
            sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + 1.2)
        return ok
    }

    private func waitHealthy(timeout: TimeInterval = 25, completion: @escaping (Bool) -> Void) {
        let deadline = Date().addingTimeInterval(timeout)
        DispatchQueue.global().async { [weak self] in
            while Date() < deadline {
                guard let self else { return }
                if self.healthOK() {
                    DispatchQueue.main.async { completion(true) }
                    return
                }
                // 子进程已经死了就不必等满超时
                if self.process?.isRunning != true { break }
                usleep(250_000)
            }
            DispatchQueue.main.async { completion(false) }
        }
    }

    // MARK: - 工具

    /// 登录 shell 的 PATH，启动时取一次缓存。取不到时退回系统 PATH + Homebrew 常见位置。
    private static let loginShellPATH: String = {
        let shell = ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/zsh"
        let lines = shellLines(shell, ["-lc", "printf %s \"$PATH\""], timeout: 3)
        let fallback = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
        guard let path = lines.first, path.contains("/") else { return fallback }
        // 系统目录必须在——登录 shell 配置再奇怪，基础命令也不能丢
        var merged = path.split(separator: ":").map(String.init)
        for dir in ["/usr/bin", "/bin", "/usr/sbin", "/sbin"] where !merged.contains(dir) {
            merged.append(dir)
        }
        return merged.joined(separator: ":")
    }()
}

/// 运行外部命令收集 stdout 行。超时杀进程返回空，绝不无限等。
private func shellLines(_ path: String, _ args: [String], timeout: TimeInterval = 5) -> [String] {
    let p = Process()
    p.executableURL = URL(fileURLWithPath: path)
    p.arguments = args
    let pipe = Pipe()
    p.standardOutput = pipe
    p.standardError = FileHandle.nullDevice
    do { try p.run() } catch { return [] }

    let deadline = Date().addingTimeInterval(timeout)
    while p.isRunning && Date() < deadline {
        usleep(50_000)
    }
    if p.isRunning {
        kill(p.processIdentifier, SIGKILL)
        return []
    }
    let data = pipe.fileHandleForReading.readDataToEndOfFile()
    guard let out = String(data: data, encoding: .utf8) else { return [] }
    return out.split(separator: "\n").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
}
