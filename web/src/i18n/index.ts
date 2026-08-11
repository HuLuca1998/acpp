import i18n from "i18next"
import LanguageDetector from "i18next-browser-languagedetector"
import { initReactI18next } from "react-i18next"

import en from "./locales/en"
import zh from "./locales/zh"

export const SUPPORTED_LANGUAGES = ["zh", "en"] as const
export type Language = (typeof SUPPORTED_LANGUAGES)[number]

export const STORAGE_KEY = "acp-language"

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      zh: { translation: zh },
      en: { translation: en },
    },
    fallbackLng: "zh",
    supportedLngs: [...SUPPORTED_LANGUAGES],
    // 只保留主语言：zh-CN / zh-TW 都落到 zh。
    load: "languageOnly",
    interpolation: { escapeValue: false },
    detection: {
      order: ["localStorage", "navigator"],
      lookupLocalStorage: STORAGE_KEY,
      caches: ["localStorage"],
    },
  })

export default i18n
