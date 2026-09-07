// Settings page的文案。独立成文件的理由同 en-db：主语言文件已到行数硬线，
// 按域拆分，不是按语言再切一刀。
export const enSettings = {
  notifications: {
    title: "System notifications",
    description:
      "Send a macOS notification when an agent stops for your decision, finishes a turn, or fails.",
    authorized: "Authorized",
    provisional: "Provisional",
    denied: "Denied",
    notDetermined: "Not asked yet",
    unknown: "Unknown",
    recheck: "Re-check",
    enable: "Enable notifications",
    openSettings: "Open System Settings",
    deniedTitle: "Notifications are denied",
    deniedDesc:
      "macOS only ever shows the permission prompt once. After a refusal the app can no longer bring it back — it has to be turned on in System Settings.",
    notInApps:
      "App is outside /Applications, so notifications cannot be authorized",
    notInAppsDesc:
      "Anywhere else the request fails outright and no system prompt appears at all. Move ACPP into Applications and reopen it. Current location: ",
  },
  desktopLaunch: {
    title: "Launch behaviour",
    description:
      "macOS desktop app only — these change this machine's login items.",
    openAtLogin: "Open at login",
    openAtLoginHint:
      "Run ACPP automatically after you log in. You can also turn this off in System Settings › General › Login Items.",
    startMinimized: "Start minimised",
    startMinimizedHint:
      "On launch, stay in the menu bar only: no window, no Dock icon. The server still starts, so it is ready when you open it.",
    failed:
      "The system refused the change: {{reason}}. Apps that are unsigned or outside the Applications folder are often rejected.",
  },
  workspace: {
    title: "Workspace root",
    description:
      "Where agents do their work: the default working directory for new sessions, and the parent of each LAN guest's own directory. Kept separate from the data directory above, which holds the database and transcripts and should never be an agent's workspace. Takes effect immediately, for sessions and guests created afterwards.",
    current: "Current",
    targetPlaceholder: "/Users/you/acpp",
    saved: "Workspace root updated",
  },
  titleModel: {
    title: "Session title generation",
    description:
      "Use a local model to turn session titles from a truncated first message into a real summary. Both claude and codex generate their titles inside their own CLIs, out of reach of the ACP channel, so a small local model does the job here — it costs no agent quota and never enters the conversation.",
    enabledLabel: "Enable",
    enabledHint:
      "When off, titles stay as the first 15 characters of the opening message",
    endpointLabel: "Ollama endpoint",
    endpointHint:
      "Address of the local ollama server; the model list reloads when you click away",
    modelLabel: "Model",
    modelPlaceholder: "Pick a model",
    modelHint:
      "Titling is a light task — smaller is faster; a 9b model usually answers in under a second on Apple Silicon",
    modelsFailed:
      "Could not load the model list — check that ollama is running",
    test: "Try it",
    save: "Save",
    saved: "Title model settings updated",
    preview: "Result: ",
  },
  menu: {
    notify: "Notifications",
    system: "System",
    env: "Environment",
    claude: "Claude",
    codex: "Codex",
    ollama: "Ollama",
    discord: "Discord",
    about: "About & Updates",
  },
  about: {
    updateTitle: "Updates",
    newVersion: "New version v{{version}}",
    check: "Check for updates",
    checkedAt: "Last checked {{time}}",
    neverChecked: "Not checked yet",
    autoCheckHint: "Auto-checks daily in the background",
    noNotes: "No release notes for this version.",
    moreVersions: "{{count}} earlier version(s) not listed",
    upToDate: "You're on the latest version.",
    apply: "Update & restart",
    applying: "Downloading & installing…",
    devHint:
      "One-click update is desktop-only; in dev mode, git pull and restart.",
    busyTitle: "Sessions are still generating",
    busyDescription:
      "{{count}} session(s) are waiting for the AI to reply. Updating now restarts the app and interrupts them — the in-flight turns never get their results and show as interrupted in history. Consider letting them finish first.",
    busyConfirm: "Update anyway",
  },
  env: {
    connTitle: "Connection test",
    connDescription:
      "Spawns the agent for real, validating the command, dependencies and login state end to end.",
    test: "Test",
    connOk: "Connected · {{count}} models available",
    depsTitle: "Dependency check",
    depsDescription:
      "Ordered by install chain: Homebrew → Node.js/npm → CLIs and ACP adapters. Everything but claude-agent-acp (which has no brew package) is versioned by Homebrew. Install what is missing and update what is behind, in one click.",
    recheck: "Re-check",
    missing: "Not installed",
    bundledHint: "Ships with Node.js",
    install: "Install",
    installing: "Installing…",
    installDone: "{{name}} installed",
    installFailed: "{{name}} install failed",
    update: "Update",
    updating: "Updating…",
    updateDone: "{{name}} updated to {{version}}",
    updateFailed: "{{name}} update failed",
    needFirst: "Install {{name}} first",
    brewManualHint:
      "Homebrew must be installed manually in a terminal (it asks for your password). Copy the command and run it there:",
    copy: "Copy command",
    copied: "Copied",
    npmLegacy: "old npm install",
    migrateHint:
      'Rows marked "old npm install" still come from a global npm install and fight Homebrew over the same command name, so nothing can be installed over them. Copy the cleanup command from that row, run it in a terminal, then hit Re-check.',
    migrateCopy: "Copy cleanup command",
    migrateCopied: "Copied — run it in a terminal, then hit Re-check",
    pathLabel: "Server PATH",
    deps: {
      brew: "Homebrew",
      node: "Node.js",
      npm: "npm",
      "claude-agent-acp": "claude-agent-acp (ACP adapter)",
      claude: "Claude Code CLI",
      "codex-acp": "codex-acp (ACP adapter)",
      codex: "Codex CLI",
    },
  },
  tool: {
    commandHint:
      "Launch command and arguments. Saving re-probes the capability catalog.",
    commandPlaceholder: "Command, e.g. claude-agent-acp",
    argsPlaceholder: "Arguments, space-separated",
    save: "Save",
    saved: "Saved — re-probing",
    missing:
      "Built-in tool record missing; restarting the server recreates it.",
  },
  system: {
    title: "Data directory",
    description:
      "Where the database and session transcripts live. Defaults to ~/.acpp, created on first launch; legacy server/data content is migrated in automatically.",
    current: "Current",
    default: "Default",
    pendingTitle: "Migrated to a new directory — restart the server to apply",
    targetPlaceholder: "Absolute path of the new data directory",
    browse: "Browse",
    migrate: "Migrate",
    confirmTitle: "Migrate data directory?",
    confirmBody:
      "A database snapshot and all session transcripts will be copied to {{dir}}. The old data stays untouched; the new directory takes effect after a server restart.",
    confirmAction: "Copy & migrate",
    cancel: "Cancel",
    done: "Migration complete — restart the server to apply",
  },
} as const
