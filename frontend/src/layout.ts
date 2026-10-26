// Uygulama iskeleti: sol menü + içerik alanı ve basit sayfa yönlendirici.
// Dil değişince iskelet ve açık sayfa yeniden çizilir.
import {t} from './i18n';
import {h, replace} from './ui/dom';
import {icon, IconName, logo} from './ui/icons';

// PageHandle, sayfanın isteğe bağlı yaşam döngüsü kancalarıdır.
export type PageHandle = {
    // canLeave false dönerse sayfadan çıkılmaz (ör. kaydedilmemiş değişiklik).
    canLeave?: () => boolean | Promise<boolean>;
    // dispose, sayfadan çıkarken çağrılır (olay aboneliklerini kaldırmak için).
    dispose?: () => void;
};

export type Page = {
    id: string;
    label: () => string;
    icon: IconName;
    render: (el: HTMLElement) => void | PageHandle | Promise<void | PageHandle>;
};

let pages: Page[] = [];
let currentId = '';
let handle: PageHandle | void;
let version = '';
let navEl: HTMLElement;
let contentEl: HTMLElement;
// renderSeq, eşzamansız render'ların sırasını korur: yavaş biten eski bir
// sayfanın handle'ı yeni sayfanınkinin üzerine yazılmaz.
let renderSeq = 0;

export function initLayout(root: HTMLElement, pageList: Page[], appVersion: string): void {
    pages = pageList;
    version = appVersion;
    currentId = pages[0].id;
    navEl = h('nav', {class: 'nav'});
    contentEl = h('main', {class: 'content'});
    replace(root,
        h('aside', {class: 'sidebar'},
            h('div', {class: 'brand'}, logo(), h('span', null, t('gui.app.title'))),
            navEl,
            h('div', {class: 'sidebar-footer'}, h('span', {class: 'version'}))),
        contentEl);
    refresh();
}

// refresh, menüyü ve açık sayfayı yeniden çizer (dil değişiminden sonra).
export function refresh(): void {
    document.querySelector('.brand span')!.textContent = t('gui.app.title');
    document.querySelector('.sidebar-footer .version')!.textContent = t('gui.app.version', {version});
    replace(navEl, pages.map((p) => h('button', {
        type: 'button',
        class: p.id === currentId ? 'nav-item active' : 'nav-item',
        on: {click: () => navigate(p.id)},
    }, icon(p.icon), h('span', null, p.label()))));
    renderCurrent();
}

// navigate, sayfaya geçer; açık sayfa izin vermezse false döner.
export async function navigate(id: string): Promise<boolean> {
    if (id === currentId) return true;
    if (handle && handle.canLeave && !(await handle.canLeave())) return false;
    currentId = id;
    navEl.querySelectorAll('.nav-item').forEach((el, i) => el.classList.toggle('active', pages[i].id === id));
    renderCurrent();
    return true;
}

function renderCurrent(): void {
    handle?.dispose?.();
    handle = undefined;
    const page = pages.find((p) => p.id === currentId)!;
    const el = h('div', {class: 'page'});
    replace(contentEl, el);
    contentEl.scrollTop = 0;
    const seq = ++renderSeq;
    Promise.resolve(page.render(el)).then((hd) => {
        if (seq === renderSeq) handle = hd;
        else hd?.dispose?.();
    }).catch(console.error);
}

// pageHeader, sayfa başlığı + sağda eylem düğmeleridir.
export function pageHeader(title: string, ...actions: HTMLElement[]): HTMLElement {
    return h('div', {class: 'page-header'}, h('h1', null, title), h('div', {class: 'actions'}, actions));
}
