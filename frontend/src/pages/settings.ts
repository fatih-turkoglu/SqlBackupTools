// Ayarlar ekranı. Şimdilik görünüm (dil, tema) ve uygulama bilgileri; diğer
// ayarlar (log saklama süresi, varsayılan klasörler) Faz 4.3'te eklenecek.
import {GetAppInfo, GetSettings, SaveSettings} from '../../wailsjs/go/app/App';
import {changeLanguage, languages, t} from '../i18n';
import {pageHeader} from '../layout';
import {applyTheme, normalizeTheme} from '../theme';
import {field, loading, select} from '../ui/components';
import {errorText, h, replace} from '../ui/dom';
import {toast} from '../ui/toast';

export async function settingsPage(el: HTMLElement): Promise<void> {
    el.append(pageHeader(t('gui.menu.settings')));
    const body = h('div', {class: 'stack'}, loading(t('gui.common.loading')));
    el.append(body);

    const [langs, settings, info] = await Promise.all([languages(), GetSettings(), GetAppInfo()]);

    const langSelect = select(
        langs.languages.map((l) => ({value: l.code, label: l.name})),
        langs.current,
        (code) => {
            // Dil değişince iskelet ve bu sayfa yeniden çizilir (onLanguageChange).
            changeLanguage(code).catch((err) => toast(errorText(err), 'error'));
        });

    const themeSelect = select([
        {value: 'dark', label: t('gui.settings.themeDark')},
        {value: 'light', label: t('gui.settings.themeLight')},
    ], normalizeTheme(settings.theme), async (theme) => {
        applyTheme(theme);
        try {
            // Başka bir alanın üzerine yazmamak için ayarlar yeniden okunur.
            const current = await GetSettings();
            await SaveSettings({...current, theme});
            toast(t('gui.settings.saved'), 'success');
        } catch (err) {
            toast(errorText(err), 'error');
        }
    });

    replace(body,
        h('section', {class: 'card'},
            h('h2', null, t('gui.settings.appearance')),
            h('div', {class: 'grid-2'},
                field(t('gui.settings.language'), langSelect),
                field(t('gui.settings.theme'), themeSelect))),
        h('section', {class: 'card'},
            h('h2', null, t('gui.settings.about')),
            h('div', {class: 'grid-2'},
                field(t('gui.settings.version'), h('span', {class: 'selectable'}, info.version)),
                field(t('gui.settings.dataDir'), h('span', {class: 'selectable mono'}, info.dataDir)))),
    );
}
