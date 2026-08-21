// 通知域文案（通知中心 / 系统通知 / 顶栏偏好开关），从 en.ts 拆出。
export const enNotify = {
  permission: "Needs your decision",
  elicitation: "A question for you",
  turnEnd: "Answer ready",
  error: "Something went wrong",
  center: {
    title: "Notification Center",
    clearAll: "Clear all",
    dismiss: "Dismiss",
    groupCount: "{{count}} notifications",
  },
  prefs: {
    title: "Notifications",
    descDesktop: "Send a system notification when a session needs you.",
    descBrowser: "Show an in-page alert when a session needs you (the tab title blinks).",
    decisions: "Decisions & questions",
    decisionsDesc: "An agent is waiting on you",
    results: "Answer ready",
    resultsDesc: "A turn finished",
    errors: "Errors",
    errorsDesc: "A turn could not finish",
    sound: "Sound",
    soundDesc: "Audible even when you are in another window",
  },
} as const
