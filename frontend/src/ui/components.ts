// Ortak bileşenler. Stiller styles/components.css içindedir. Metinler
// çağıran tarafından t(...) ile verilir; bileşenler çeviri yapmaz.
import {Child, h} from './dom';
import {icon, IconName} from './icons';

// ---------- Buton ----------

export type ButtonOptions = {
    variant?: 'primary' | 'danger' | 'ghost';
    small?: boolean;
    icon?: IconName;
    title?: string;
    disabled?: boolean;
    // onClick Promise dönerse buton iş bitene kadar meşgul gösterilir
    // (spinner + devre dışı), böylece çift tıklama engellenir.
    onClick?: (ev: MouseEvent) => void | Promise<unknown>;
};

export function button(label: string, opts: ButtonOptions = {}): HTMLButtonElement {
    const cls = ['btn', opts.variant && `btn-${opts.variant}`, opts.small && 'btn-sm'].filter(Boolean).join(' ');
    const btn = h('button', {type: 'button', class: cls, title: opts.title, disabled: opts.disabled},
        opts.icon && icon(opts.icon), label && h('span', null, label));
    if (opts.onClick) {
        btn.addEventListener('click', (ev) => {
            const r = opts.onClick!(ev);
            if (r instanceof Promise) {
                setBusy(btn, true);
                r.finally(() => setBusy(btn, false));
            }
        });
    }
    return btn;
}

// setBusy, butonu meşgul (simge yerine spinner, devre dışı) veya normal
// duruma getirir.
export function setBusy(btn: HTMLButtonElement, busy: boolean): void {
    btn.disabled = busy;
    btn.classList.toggle('busy', busy);
    const sp = btn.querySelector(':scope > .spinner');
    if (busy && !sp) btn.prepend(spinner());
    if (!busy) sp?.remove();
}

// ---------- Form alanları ----------

export type FieldOptions = {
    hint?: string;
    error?: string;
};

// field, etiket + kontrol + açıklama/hata satırından oluşan alan sarmalayıcısıdır.
export function field(label: string, control: Child, opts: FieldOptions = {}): HTMLLabelElement {
    const el = h('label', {class: 'field'},
        label && h('span', {class: 'field-label'}, label),
        control,
        h('span', {class: 'field-hint', hidden: !opts.hint}, opts.hint ?? ''),
        h('span', {class: 'field-error', hidden: true}),
    );
    setFieldError(el, opts.error);
    return el;
}

// setFieldError, alanın hata metnini gösterir; boş metin hatayı kaldırır.
export function setFieldError(fieldEl: Element, error?: string): void {
    const errEl = fieldEl.querySelector<HTMLElement>('.field-error')!;
    errEl.textContent = error ?? '';
    errEl.hidden = !error;
    fieldEl.classList.toggle('invalid', !!error);
}

export type InputOptions = {
    type?: 'text' | 'password' | 'number';
    value?: string | number;
    placeholder?: string;
    disabled?: boolean;
    min?: number;
    max?: number;
    mono?: boolean;
    onInput?: (value: string) => void;
};

export function input(opts: InputOptions = {}): HTMLInputElement {
    const el = h('input', {
        class: opts.mono ? 'input mono' : 'input',
        type: opts.type ?? 'text',
        value: opts.value === undefined ? '' : String(opts.value),
        placeholder: opts.placeholder,
        disabled: opts.disabled,
        min: opts.min,
        max: opts.max,
        spellcheck: false,
        autocomplete: 'off',
    });
    if (opts.onInput) el.addEventListener('input', () => opts.onInput!(el.value));
    return el;
}

export type SelectOption = { value: string; label: string };

export function select(options: SelectOption[], value: string, onChange?: (value: string) => void): HTMLSelectElement {
    const el = h('select', {class: 'select'},
        options.map((o) => h('option', {value: o.value, selected: o.value === value}, o.label)));
    if (onChange) el.addEventListener('change', () => onChange(el.value));
    return el;
}

// checkbox ve toggle (switch) aynı imzaya sahiptir; dönen etiketin içindeki
// input'a .querySelector('input') ile erişilebilir.
export function checkbox(label: Child, checked: boolean, onChange?: (checked: boolean) => void): HTMLLabelElement {
    return checkLike('check', label, checked, onChange);
}

export function toggle(label: Child, checked: boolean, onChange?: (checked: boolean) => void): HTMLLabelElement {
    return checkLike('switch', label, checked, onChange);
}

function checkLike(cls: string, label: Child, checked: boolean, onChange?: (checked: boolean) => void): HTMLLabelElement {
    const box = h('input', {type: 'checkbox', checked});
    if (onChange) box.addEventListener('change', () => onChange(box.checked));
    return h('label', {class: cls}, box, label !== '' && h('span', null, label));
}

// ---------- Tablo ----------

export type Column<T> = {
    title: string;
    // render hücre içeriğini üretir; yoksa satırın key alanı metin olarak yazılır.
    key?: keyof T;
    render?: (row: T) => Child;
    // num: sağa hizalı sayı; shrink: içeriğe göre dar sütun (ör. butonlar).
    className?: 'num' | 'shrink';
};

export type TableOptions<T> = {
    empty?: string;
    onRowClick?: (row: T) => void;
};

export function table<T>(columns: Column<T>[], rows: T[], opts: TableOptions<T> = {}): HTMLElement {
    if (rows.length === 0 && opts.empty) {
        return h('div', {class: 'table-wrap'}, h('div', {class: 'empty'}, opts.empty));
    }
    const head = h('tr', null, columns.map((c) => h('th', {class: c.className}, c.title)));
    const body = rows.map((row) => {
        const tr = h('tr', {class: opts.onRowClick ? 'clickable' : undefined},
            columns.map((c) => h('td', {class: c.className},
                c.render ? c.render(row) : c.key !== undefined ? String(row[c.key] ?? '') : '')));
        if (opts.onRowClick) {
            tr.addEventListener('click', (ev) => {
                // Satırdaki buton/giriş tıklamaları satır tıklaması sayılmaz.
                if (!(ev.target as Element).closest('button, input, select, a, label')) opts.onRowClick!(row);
            });
        }
        return tr;
    });
    return h('div', {class: 'table-wrap'},
        h('table', {class: 'table'}, h('thead', null, head), h('tbody', null, body)));
}

// ---------- Durum göstergeleri ----------

export type Tone = 'success' | 'warning' | 'error' | 'accent' | 'info';

export function badge(text: string, tone?: Exclude<Tone, 'info'>): HTMLSpanElement {
    return h('span', {class: tone ? `badge badge-${tone}` : 'badge'}, text);
}

// alert, satır içi bilgi/uyarı/hata kutusudur; items verilirse liste olarak eklenir.
export function alert(tone: Exclude<Tone, 'accent'>, message: Child, items?: string[]): HTMLDivElement {
    return h('div', {class: `alert alert-${tone}`},
        message,
        items && items.length > 0 && h('ul', null, items.map((i) => h('li', null, i))));
}

export function spinner(): HTMLSpanElement {
    return h('span', {class: 'spinner', role: 'status'});
}

// loading, sayfa/bölüm yüklenirken gösterilen spinner + metindir.
export function loading(text: string): HTMLDivElement {
    return h('div', {class: 'loading'}, spinner(), text);
}

export type ProgressBar = {
    el: HTMLDivElement;
    // set: 0-100 arası yüzde; null belirsiz (kayan) ilerleme gösterir.
    set(percent: number | null): void;
    state(state: 'running' | 'success' | 'error'): void;
};

export function progress(percent: number | null = null): ProgressBar {
    const bar = h('div', {class: 'progress-bar'});
    const el = h('div', {class: 'progress', role: 'progressbar'}, bar);
    const p: ProgressBar = {
        el,
        set(value) {
            el.classList.toggle('indeterminate', value === null);
            if (value === null) {
                bar.style.width = '';
                el.removeAttribute('aria-valuenow');
            } else {
                const v = Math.max(0, Math.min(100, value));
                bar.style.width = `${v}%`;
                el.setAttribute('aria-valuenow', String(v));
            }
        },
        state(state) {
            el.classList.toggle('success', state === 'success');
            el.classList.toggle('error', state === 'error');
        },
    };
    p.set(percent);
    return p;
}
