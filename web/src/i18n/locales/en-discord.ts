/** Discord channel workspaces (adr-016) copy, kept out of en.ts (line budget). */
export const enDiscord = {
  settings: {
    title: "Discord Bot",
    description:
      "Bind Discord channels to git repo workspaces: run /init in a channel to pick a repo and model; it clones into a dedicated discord workspace. Fully independent from web sessions.",
    enable: "Enable Discord",
    enableDesc:
      "Takes effect immediately: on brings the bot online, off disconnects it.",
    token: "Bot token",
    tokenSet: "Configured",
    tokenPlaceholder: "Paste the bot token from the Discord developer portal",
    tokenReplacePlaceholder: "Saved — paste a new token to replace",
    workRoot: "Workspace root",
    workRootDesc: "Where channel repos are cloned, as <root>/<org>/<repo>.",
    save: "Save",
    saved: "Saved",
    saveFailed: "Save failed",
    status: {
      disabled: "Disabled",
      waitingToken: "Enabled, waiting for a token",
      connecting: "Connecting…",
      online: "Online: {{name}}",
      error: "Connection failed",
    },
    guilds: "Servers",
    noGuilds:
      "The bot isn't in any server yet — use the install link in the Discord developer portal to invite it.",
    refresh: "Refresh status",
    howTo:
      "Type /init in a channel to bind a repo and model; manage bindings on the Discord page.",
  },
  page: {
    description:
      "Channel ↔ repo workspace bindings. Bindings are created with /init inside Discord channels; edit the model, thinking depth, or unbind here.",
    botOffline: "Bot offline",
    botOnline: "Online: {{name}}",
    gotoSettings: "Open settings",
    empty: "No channel bindings yet",
    emptyHint: "Type /init in a Discord channel and pick a repo and model.",
    channel: "Channel",
    repo: "Repository",
    workdir: "Workdir",
    model: "Model",
    effort: "Thinking depth",
    effortDefault: "Default",
    access: "Access level",
    accessSafe: "Safe (confirm each write)",
    accessAutoEdit: "Auto edit",
    accessFull: "Full access",
    updatedAt: "Updated",
    edit: "Edit",
    editTitle: "Edit binding",
    editDesc:
      "Change the model and thinking depth for this channel. To switch repos, run /init in the channel again.",
    unbind: "Unbind",
    unbindTitle: "Unbind this channel?",
    unbindDesc:
      "The binding between {{channel}} and {{repo}} will be removed; the clone on disk is kept.",
    unbindConfirm: "Unbind",
    saved: "Saved",
    saveFailed: "Save failed",
    unbound: "Unbound",
    unbindFailed: "Unbind failed",
  },
}
