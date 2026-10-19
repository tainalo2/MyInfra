import { useEffect, useState } from "react";
import { currentLang, setLang, subscribeLang, text, type Lang, type MsgKey } from "./i18n";

export function useI18n() {
  const [lang, setCurrent] = useState<Lang>(currentLang);

  useEffect(() => subscribeLang(() => setCurrent(currentLang())), []);

  return {
    lang,
    setLang,
    t: (key: MsgKey, params?: Record<string, string | number>) => text(key, params),
  };
}
