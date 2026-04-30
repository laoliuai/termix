import { signal } from "@preact/signals";
import { defaultLocale, messages, type Locale, type MessageKey } from "./messages";

const STORAGE_KEY = "termix.locale";

function isLocale(value: string | null): value is Locale {
  return value === "en" || value === "zh-CN";
}

function browserLocale(): Locale {
  const lang = navigator.language.toLowerCase();
  return lang.startsWith("zh") ? "zh-CN" : defaultLocale;
}

function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (isLocale(saved)) return saved;
    if (saved !== null) return defaultLocale;
  } catch {
    return browserLocale();
  }
  return browserLocale();
}

export const locale = signal<Locale>(initialLocale());

export function setLocale(next: Locale): void {
  locale.value = next;
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // Private browsing storage failures should not block language switching.
  }
}

export function t(key: MessageKey): string {
  return messages[locale.value][key] ?? messages[defaultLocale][key];
}
