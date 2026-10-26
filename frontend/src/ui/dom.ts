// Küçük DOM yardımcıları. Arayüz innerHTML yerine bunlarla kurulur; böylece
// sunucu/veritabanı adları, hata metinleri gibi dış veriler HTML olarak
// yorumlanmaz (metin düğümü olarak eklenir).

export type Child = Node | string | number | null | undefined | false | Child[];

type Attrs = {
    class?: string;
    style?: Partial<CSSStyleDeclaration>;
    dataset?: Record<string, string>;
    // on: {click: fn} → addEventListener
    on?: { [K in keyof HTMLElementEventMap]?: (ev: HTMLElementEventMap[K]) => void };
    [attr: string]: unknown;
};

// h, eleman oluşturur: h('div', {class: 'card'}, 'metin', h('b', null, 'x')).
// Elemanın metin dışı özellikleri (checked, disabled, onclick yerine on …) ve
// value özellik olarak, diğerleri öznitelik olarak atanır; false/null/
// undefined atlanır.
export function h<K extends keyof HTMLElementTagNameMap>(
    tag: K,
    attrs?: Attrs | null,
    ...children: Child[]
): HTMLElementTagNameMap[K] {
    const el = document.createElement(tag);
    if (attrs) {
        for (const [key, value] of Object.entries(attrs)) {
            if (value === undefined || value === null || value === false) continue;
            if (key === 'class') {
                el.className = value as string;
            } else if (key === 'style') {
                Object.assign(el.style, value);
            } else if (key === 'dataset') {
                Object.assign(el.dataset, value);
            } else if (key === 'on') {
                for (const [name, fn] of Object.entries(value as Record<string, EventListener>)) {
                    el.addEventListener(name, fn);
                }
            } else if (key in el && (typeof value !== 'string' || key === 'value')) {
                (el as any)[key] = value;
            } else {
                el.setAttribute(key, value === true ? '' : String(value));
            }
        }
    }
    append(el, children);
    return el;
}

export function append(parent: Node, children: Child[]): void {
    for (const c of children) {
        if (c === null || c === undefined || c === false) continue;
        if (Array.isArray(c)) {
            append(parent, c);
        } else {
            parent.appendChild(c instanceof Node ? c : document.createTextNode(String(c)));
        }
    }
}

// replace, elemanın içeriğini verilen çocuklarla değiştirir.
export function replace(parent: Element, ...children: Child[]): void {
    parent.replaceChildren();
    append(parent, children);
}

// svg, sabit (güvenilir) SVG işaretlemesinden eleman üretir; yalnızca
// ui/icons.ts gibi koddaki sabitler için kullanılır, dış veri için değil.
export function svg(markup: string): SVGSVGElement {
    const tpl = document.createElement('template');
    tpl.innerHTML = markup.trim();
    return tpl.content.firstElementChild as SVGSVGElement;
}

// errorText, Wails'ten gelen Promise reddini metne çevirir (Go hataları
// metin olarak gelir).
export function errorText(err: unknown): string {
    if (typeof err === 'string') return err;
    if (err instanceof Error) return err.message;
    return String(err);
}
