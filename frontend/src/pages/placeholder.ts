// Henüz yazılmamış ekranlar için geçici sayfa (Faz 4.3'te kaldırılacak).
import {t} from '../i18n';
import {pageHeader} from '../layout';
import {h} from '../ui/dom';

export function placeholderPage(title: () => string) {
    return (el: HTMLElement) => {
        el.append(
            pageHeader(title()),
            h('div', {class: 'card'}, h('div', {class: 'empty'}, t('gui.page.underDevelopment'))),
        );
    };
}
