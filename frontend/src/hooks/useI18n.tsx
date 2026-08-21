import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { pageCopy, translate, type Lang } from '@/i18n/messages'

type I18nContextValue = {
  lang: Lang
  setLang: (lang: Lang) => void
  t: (key: string, vars?: Record<string, string | number>) => string
  page: (name: string) => { kicker: string; title: string; desc: string }
}

const I18nContext = createContext<I18nContextValue | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => {
    const saved = localStorage.getItem('cli2api_lang')
    return saved === 'en' ? 'en' : 'zh'
  })

  const value = useMemo<I18nContextValue>(() => ({
    lang,
    setLang: (next) => {
      const resolved = next === 'en' ? 'en' : 'zh'
      localStorage.setItem('cli2api_lang', resolved)
      document.documentElement.lang = resolved === 'zh' ? 'zh-CN' : 'en'
      setLangState(resolved)
    },
    t: (key, vars) => translate(lang, key, vars),
    page: (name) => pageCopy(lang, name),
  }), [lang])

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n() {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used within I18nProvider')
  return ctx
}
