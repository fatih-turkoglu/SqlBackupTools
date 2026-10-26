import './styles/theme.css';
import './styles/base.css';
import './styles/layout.css';
import './styles/components.css';

import {GetAppInfo, GetSettings} from '../wailsjs/go/app/App';
import {loadTranslations, onLanguageChange, t} from './i18n';
import {initLayout, Page, refresh} from './layout';
import {placeholderPage} from './pages/placeholder';
import {settingsPage} from './pages/settings';
import {applyCachedTheme, applyTheme} from './theme';
import {errorText} from './ui/dom';
import {toast} from './ui/toast';

const pages: Page[] = [
    {id: 'jobs', label: () => t('gui.menu.jobs'), icon: 'jobs', render: placeholderPage(() => t('gui.menu.jobs'))},
    {id: 'logs', label: () => t('gui.menu.logs'), icon: 'logs', render: placeholderPage(() => t('gui.menu.logs'))},
    {id: 'settings', label: () => t('gui.menu.settings'), icon: 'settings', render: settingsPage},
];

async function start(): Promise<void> {
    applyCachedTheme();
    const [info, settings] = await Promise.all([GetAppInfo(), GetSettings().catch(() => null), loadTranslations()]);
    applyTheme(settings?.theme ?? '');
    initLayout(document.getElementById('app')!, pages, info.version);
    onLanguageChange(refresh);
    // Veri klasörü/ayarlar sorunu (ör. bozuk settings.json): uygulama
    // varsayılanlarla çalışır, kullanıcı bilgilendirilir.
    if (info.startupError) toast(info.startupError, 'error');
}

start().catch((err) => {
    console.error(err);
    toast(errorText(err), 'error');
});
