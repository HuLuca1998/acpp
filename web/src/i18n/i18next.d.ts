import type en from "./locales/en"

// 让 t("chat.send") 这类 key 受类型检查，写错 key 会在编译期报错。
declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation"
    resources: {
      translation: typeof en
    }
  }
}
