// 通知域文案（通知中心 / 系统通知 / 顶栏偏好开关），从 zh.ts 拆出。
export const zhNotify = {
  permission: "需要你的决策",
  elicitation: "有问题要问你",
  turnEnd: "回答完成",
  error: "出错了",
  center: {
    title: "通知中心",
    clearAll: "全部清除",
    dismiss: "清除这条",
    groupCount: "{{count}} 个通知",
  },
  prefs: {
    title: "通知",
    descDesktop: "会话有动静时发一条系统通知。",
    descBrowser: "会话有动静时在页面上提醒（标签页标题会闪烁）。",
    decisions: "决策与问答",
    decisionsDesc: "agent 停下来等你回应",
    results: "回答完成",
    resultsDesc: "一轮跑完了",
    errors: "出错",
    errorsDesc: "这一轮没能跑完",
    sound: "提示音",
    soundDesc: "人在别的窗口时也听得到",
  },
} as const
