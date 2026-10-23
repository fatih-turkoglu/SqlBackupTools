// Arayüz metinleri backend'deki dil dosyalarından (lang\<kod>.lng) gelir.
// Kullanım:
//   t('gui.menu.jobs')                      → metin
//   t('gui.jobs.count', {count: 3})         → "{count}" yer tutucusu doldurulur
//   <span data-i18n="gui.menu.jobs"></span> → applyI18n() ile doldurulur
//   data-i18n-title / data-i18n-placeholder → title / placeholder öznitelikleri
// Dil değişince onLanguageChange ile kaydolan fonksiyonlar çağrılır; ekran
// yeniden başlatma gerekmeden yeniden çizilir.
import {GetLanguages, GetTranslations, SetLanguage} from '../wailsjs/go/app/App';

let dict: Record<string, string> = {};
const listeners: Array<() => void> = [];

export async function loadTranslations(): Promise<void> {
    dict = await GetTranslations();
}

export function t(key: string, params?: Record<string, string | number>): string {
    const s = dict[key] ?? key;
    if (!params) return s;
    // Tek geçişte doldurulur; değer içindeki "{...}" yeniden işlenmez.
    return s.replace(/\{(\w+)\}/g, (m, name) => (name in params ? String(params[name]) : m));
}

export function applyI18n(root: ParentNode = document): void {
    root.querySelectorAll<HTMLElement>('[data-i18n]').forEach((el) => {
        el.textContent = t(el.dataset.i18n!);
    });
    root.querySelectorAll<HTMLElement>('[data-i18n-title]').forEach((el) => {
        el.title = t(el.dataset.i18nTitle!);
    });
    root.querySelectorAll<HTMLInputElement>('[data-i18n-placeholder]').forEach((el) => {
        el.placeholder = t(el.dataset.i18nPlaceholder!);
    });
}

export function onLanguageChange(fn: () => void): void {
    listeners.push(fn);
}

export async function languages() {
    return GetLanguages();
}

// changeLanguage, dili değiştirir (ayarlara kaydedilir), metinleri yeniden
// yükler ve kayıtlı yeniden çizim fonksiyonlarını çağırır.
export async function changeLanguage(code: string): Promise<void> {
    await SetLanguage(code);
    await loadTranslations();
    listeners.forEach((fn) => fn());
}
