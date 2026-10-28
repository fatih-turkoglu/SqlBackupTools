// Ayarlar ekranı: görünüm (dil, tema), varsayılanlar (yeni işlerin yedek
// klasörleri, log saklama süresi) ve uygulama bilgileri.
import {GetAppInfo, GetSettings, SaveSettings} from '../../wailsjs/go/app/App';
import {changeLanguage, languages, t} from '../i18n';
import {pageHeader} from '../layout';
import {applyTheme, normalizeTheme} from '../theme';
import {button, field, input, loading, select} from '../ui/components';
import {errorText, h, replace} from '../ui/dom';
import {toast} from '../ui/toast';
import {folderInput} from './common';

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

    // Varsayılanlar ayrı "Kaydet" ile yazılır (dil ve tema anında kaydedilir).
    const defaults = {
        logKeepDays: settings.logKeepDays,
        defaultServerPath: settings.defaultServerPath ?? '',
        defaultAccessPath: settings.defaultAccessPath ?? '',
    };
    const serverPath = folderInput(defaults.defaultServerPath, (v) => defaults.defaultServerPath = v.trim());
    const accessPath = folderInput(defaults.defaultAccessPath, (v) => defaults.defaultAccessPath = v.trim());
    const keepDays = input({
        type: 'number',
        value: settings.logKeepDays || '',
        min: 0,
        placeholder: '30',
        onInput: (v) => defaults.logKeepDays = Math.max(0, parseInt(v, 10) || 0),
    });
    const saveDefaults = async () => {
        try {
            const current = await GetSettings();
            await SaveSettings({...current, ...defaults});
            toast(t('gui.settings.saved'), 'success');
        } catch (err) {
            toast(errorText(err), 'error');
        }
    };

    replace(body,
        h('section', {class: 'card'},
            h('h2', null, t('gui.settings.appearance')),
            h('div', {class: 'grid-2'},
                field(t('gui.settings.language'), langSelect),
                field(t('gui.settings.theme'), themeSelect))),
        h('section', {class: 'card'},
            h('h2', null, t('gui.settings.defaults')),
            h('div', {class: 'stack'},
                field(t('gui.settings.defaultServerPath'), serverPath.el, {hint: t('gui.settings.defaultServerPathHint')}),
                field(t('gui.settings.defaultAccessPath'), accessPath.el, {hint: t('gui.settings.defaultAccessPathHint')}),
                h('div', {class: 'grid-2'},
                    field(t('gui.settings.logKeepDays'), keepDays, {hint: t('gui.settings.logKeepDaysHint')})),
                h('div', {class: 'row'},
                    button(t('gui.settings.saveDefaults'), {variant: 'primary', icon: 'check', onClick: saveDefaults})))),
        h('section', {class: 'card'},
            h('h2', null, t('gui.settings.about')),
            h('div', {class: 'grid-2'},
                field(t('gui.settings.version'), h('span', {class: 'selectable'}, info.version)),
                field(t('gui.settings.dataDir'), h('span', {class: 'selectable mono'}, info.dataDir)))),
    );
}
