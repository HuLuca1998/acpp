// Server page copy (adr-019). Split out for the same reason as en-db:
// the main locale file is at its line limit, so split by domain.
export const enServer = {
  title: "Servers",
  description:
    "Add a machine and the AI can read its files, containers and load — no more sshing in yourself to see how a deploy actually went. Datasource SSH tunnels use these records too.",
  add: "New server",
  edit: "Edit server",
  name: "Name",
  searchPlaceholder: "Name / host",
  namePlaceholder: "pp-game-live",
  nameHint: "This is what the AI passes to its tools; no spaces or slashes",
  host: "Host",
  port: "Port",
  user: "Username",
  auth: "Authentication",
  authPassword: "Password",
  authKey: "Public key",
  authBoth: "Password and key",
  password: "Password",
  passwordKeep: "Leave blank to keep current",
  sshKey: "SSH key",
  sshKeyNone: "None (use a path or ssh-agent)",
  sshKeyHint:
    "Pick one from the key library — it travels with your config, so there is no file to find on the next machine.",
  keyPath: "Private key path",
  keyPathPlaceholder: "Leave blank to use ssh-agent",
  keyBrowse: "Choose private key",
  passphrase: "Passphrase",
  note: "Note",
  notePlaceholder: "pp-game production, project at /srv/pp-game-live",
  noteHint:
    "Shown to the AI: say what this machine is for and where the project lives — it saves several rounds of guessing",
  disabled: "Disabled",
  disabledHint:
    "Disabling only hides it from the AI; datasources that tunnel through it keep working",
  enabled: "Enabled",
  test: "Test connection",
  empty: "No servers configured",
  emptyHint: "Add a machine and the AI can observe it.",
  deleteTitle: "Delete server",
  deleteConfirm: "Delete \u201c{{name}}\u201d?",
  usedByHint: "{{count}} datasource(s) tunnel through this machine",
  deleteInUse: "A datasource still uses it as a tunnel host; change that first",
  deleted: "Deleted",
  saved: "Saved",
  loadFailed: "Failed to load",
  knownHosts:
    "The host fingerprint is recorded in ~/.ssh/known_hosts on first connect. A changed fingerprint is refused outright \u2014 that is the one real man-in-the-middle signal, so there is no skip switch.",
  scopeWarning:
    "Once configured, the AI can read any path on this machine \u2014 there is no per-project isolation. To narrow it down, give it a restricted SSH account.",
  pickForTunnel: "Tunnel host",
  pickPlaceholder: "Choose a server\u2026",
  pickEmpty: "No servers configured",
  pickCreate: "New server",
  pickManage: "Manage servers",
  pickHint:
    "The tunnel dials the host above from this machine (production databases are usually 127.0.0.1:3306).",
  refServer: "Server",
  refTitle: "Reference a server",
  refHint:
    "Hand the AI one machine — it will target this host for the turn and actually look at its containers, logs and load with read-only tools.",
} as const

// 私钥库：与服务器同一功能域（服务器引用这些钥匙），主 locale 文件到了
// 行数硬线，按域拆在这里。
export const enSSHKeys = {
  title: "SSH keys",
  add: "New key",
  hint: "One key can open several machines; the private key lives in the library, so it travels with your config.",
  empty: "No keys yet",
  emptyHint:
    "Generate one, paste an existing key, or import it from a local file.",
  name: "Name",
  namePlaceholder: "deploy-key",
  fingerprint: "Fingerprint",
  searchPlaceholder: "name / note / fingerprint",
  edit: "Edit",
  deleteInUse: "Still used by servers — change those first",
  nameHint:
    "Shown in the server form's picker, and used as the generated key's comment.",
  note: "Note",
  notePlaceholder: "Shared deploy key",
  source: "Key source",
  sourceGenerate: "Generate",
  sourcePaste: "Paste",
  sourceFile: "From file",
  sourceHint: {
    generate:
      "Creates an ed25519 key here; copy its public key onto the target machine.",
    paste: "Paste the whole -----BEGIN OPENSSH PRIVATE KEY----- block.",
    file: "Reads a local key file once and stores its content — the path stops mattering afterwards.",
  },
  privateKey: "Private key",
  privateKeyKeep: "Leave empty to keep the current one",
  keyPath: "Key file path",
  keyPathHint: "Read once on import; the content is stored in the library.",
  passphrase: "Passphrase",
  passphraseKeep: "Leave empty to keep the current one",
  passphraseHint:
    "Needed for an encrypted key; a generated key is encrypted with it too.",
  hasPassphrase: "Has passphrase",
  usedBy: "used by {{count}} server(s)",
  unused: "Not used by any server",
  copyPublicKey: "Copy public key",
  publicKeyCopied:
    "Public key copied — paste it into the target machine's authorized_keys",
  created: "Key created",
  saved: "Saved",
  deleted: "Key deleted",
  deleteTitle: "Delete key",
  deleteBody:
    "{{name}} will be removed for good. Refused while servers still use it.",
  editTitle: "Edit key",
  addTitle: "New key",
  dialogHint:
    "The private key is stored in the library and travels with your connection config.",
}
