// Ekranlarda ortak kullanılan biçimlendirme yardımcıları. Tarih/saat Windows
// bölge ayarlarına göre yazılır.
import {config} from '../wailsjs/go/models';
import {t} from './i18n';
import {badge} from './ui/components';

// toDate, Go time.Time değerini (ISO metin) Date'e çevirir; boş veya sıfır
// zaman (0001-01-01) için null döner.
export function toDate(v: unknown): Date | null {
    if (!v) return null;
    const d = new Date(v as string);
    if (isNaN(d.getTime()) || d.getFullYear() < 1900) return null;
    return d;
}

export function fmtDate(v: unknown): string {
    const d = toDate(v);
    return d ? d.toLocaleString(undefined, {dateStyle: 'short', timeStyle: 'short'}) : '';
}

export function fmtDay(v: unknown): string {
    const d = toDate(v);
    return d ? d.toLocaleDateString(undefined, {dateStyle: 'medium'}) : '';
}

export function fmtBytes(n: number): string {
    if (!n || n < 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
        n /= 1024;
        i++;
    }
    return `${i === 0 ? n : n.toFixed(1)} ${units[i]}`;
}

// fmtMB, msdb'den gelen MB cinsinden boyuttur.
export function fmtMB(mb: number): string {
    return fmtBytes(mb * 1024 * 1024);
}

// fmtDuration: 1 dakikanın altı "12.3 s", üstü "m:ss" veya "h:mm:ss".
export function fmtDuration(ms: number): string {
    if (!ms || ms < 0) return '0 s';
    if (ms < 60000) return `${(ms / 1000).toFixed(1)} s`;
    const total = Math.round(ms / 1000);
    const s = total % 60, m = Math.floor(total / 60) % 60, hrs = Math.floor(total / 3600);
    const pad = (x: number) => String(x).padStart(2, '0');
    return hrs > 0 ? `${hrs}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

// Haftanın günleri (config.Schedule.Days değerleri) sırasıyla.
export const weekDays = ['MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT', 'SUN'] as const;

export function dayName(day: string): string {
    switch (day) {
        case 'MON': return t('gui.day.mon');
        case 'TUE': return t('gui.day.tue');
        case 'WED': return t('gui.day.wed');
        case 'THU': return t('gui.day.thu');
        case 'FRI': return t('gui.day.fri');
        case 'SAT': return t('gui.day.sat');
        case 'SUN': return t('gui.day.sun');
    }
    return day;
}

export function scheduleText(s: config.Schedule): string {
    switch (s.frequency) {
        case 'weekly': {
            const days = weekDays.filter((d) => s.days?.includes(d)).map(dayName).join(', ');
            return t('gui.schedule.weeklyText', {days, time: s.time});
        }
        case 'hourly':
            return t('gui.schedule.hourlyText', {hours: s.intervalHours, time: s.time});
    }
    return t('gui.schedule.dailyText', {time: s.time});
}

// exitBadge, çalışma sonucunun rozetidir (exit code sözleşmesi:
// 0 başarılı, 1 kısmi, 2 başarısız, 3 zaten çalışıyordu).
export function exitBadge(code: number, alreadyRunning = false): HTMLSpanElement {
    if (alreadyRunning || code === 3) return badge(t('gui.run.alreadyRunning'), 'accent');
    switch (code) {
        case 0: return badge(t('gui.run.success'), 'success');
        case 1: return badge(t('gui.run.partial'), 'warning');
    }
    return badge(t('gui.run.failed'), 'error');
}

// dbStatusBadge, veritabanı sonucunun (backup.Status) rozetidir.
export function dbStatusBadge(status: string): HTMLSpanElement {
    switch (status) {
        case 'success': return badge(t('gui.run.dbSuccess'), 'success');
        case 'skipped': return badge(t('gui.run.dbSkipped'), 'warning');
    }
    return badge(t('gui.run.dbFailed'), 'error');
}
