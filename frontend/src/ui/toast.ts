// Sağ altta kısa süreli bildirimler. Tıklayınca kapanır; hatalar daha uzun
// kalır. Metin düz metindir (satır sonları korunur).
import {h} from './dom';

export type ToastKind = 'info' | 'success' | 'warning' | 'error';

const durations: Record<ToastKind, number> = {info: 4000, success: 4000, warning: 7000, error: 9000};
const maxToasts = 5;

let container: HTMLDivElement | null = null;

export function toast(message: string, kind: ToastKind = 'info'): void {
    if (!container) {
        container = h('div', {class: 'toasts', role: 'status', 'aria-live': 'polite'});
        document.body.appendChild(container);
    }
    const el = h('div', {class: `toast ${kind}`}, message);
    const close = () => {
        if (el.classList.contains('leaving')) return;
        el.classList.add('leaving');
        setTimeout(() => el.remove(), 200);
    };
    el.addEventListener('click', close);
    container.appendChild(el);
    while (container.childElementCount > maxToasts) container.firstElementChild!.remove();
    setTimeout(close, durations[kind]);
}
