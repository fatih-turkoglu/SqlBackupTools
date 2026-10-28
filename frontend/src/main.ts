import './styles/theme.css';
import './styles/base.css';
import './styles/layout.css';
import './styles/components.css';

import {GetAppInfo, GetSettings} from '../wailsjs/go/app/App';
import {loadTranslations, onLanguageChange, t} from './i18n';
import {initLayout, Page, refresh} from './layout';
import {jobsPage} from './pages/jobs';
import {logsPage} from './pages/logs';
import {settingsPage} from './pages/settings';
import {initRuns} from './runs';
import {applyCachedTheme, applyTheme} from './theme';
import {errorText} from './ui/dom';
import {toast} from './ui/toast';

const pages: Page[] = [
    {id: 'jobs', label: () => t('gui.menu.jobs'), icon: 'jobs', render: jobsPage},
    {id: 'logs', label: () => t('gui.menu.logs'), icon: 'logs', render: logsPage},
    {id: 'settings', label: () => t('gui.menu.settings'), icon: 'settings', render: settingsPage},
];

async function start(): Promise<void> {
    applyCachedTheme();
    const [info, settings] = await Promise.all([GetAppInfo(), GetSettings().catch(() => null), loadTranslations()]);
    applyTheme(settings?.theme ?? '');
    initRuns();
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
