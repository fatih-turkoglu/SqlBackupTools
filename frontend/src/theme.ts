// Tema: <html data-theme="dark|light"> (styles/theme.css) + pencere başlık
// çubuğu. Kalıcı değer settings.json'daki "theme"dir; açılışta ayarlar
// gelene kadar yanlış temanın görünmemesi için son tema localStorage'da da
// tutulur.
import {WindowSetDarkTheme, WindowSetLightTheme} from '../wailsjs/runtime/runtime';

export type Theme = 'dark' | 'light';

const cacheKey = 'sqlbackup.theme';

export function normalizeTheme(theme: string | null | undefined): Theme {
    return theme === 'light' ? 'light' : 'dark';
}

// applyCachedTheme, son kullanılan temayı ayarlar beklemeden uygular.
export function applyCachedTheme(): void {
    let cached: string | null = null;
    try {
        cached = localStorage.getItem(cacheKey);
    } catch {
        // localStorage kullanılamıyorsa varsayılan tema kalır.
    }
    document.documentElement.dataset.theme = normalizeTheme(cached);
}

export function applyTheme(theme: string): void {
    const value = normalizeTheme(theme);
    document.documentElement.dataset.theme = value;
    try {
        localStorage.setItem(cacheKey, value);
    } catch {
        // Önbellek yalnızca kolaylık; yazılamazsa sorun değil.
    }
    try {
        if (value === 'light') WindowSetLightTheme();
        else WindowSetDarkTheme();
    } catch {
        // Wails runtime'ı yoksa (tarayıcıda geliştirme) yalnızca sayfa teması.
    }
}
