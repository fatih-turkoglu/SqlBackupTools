// Loglar ekranı: çalışma geçmişi (JSON çalışma kayıtları) ve günlük log
// dosyaları. İşler ekranından bir işin geçmişi showJobLogs ile açılır.
import {ListJobs, ListLogs, ListRuns, ReadLog, ReadRun} from '../../wailsjs/go/app/App';
import {app, logging} from '../../wailsjs/go/models';
import {exitBadge, fmtBytes, fmtDate, fmtDay, fmtDuration} from '../format';
import {t} from '../i18n';
import {navigate, pageHeader} from '../layout';
import {resultView} from '../runs';
import {alert, badge, button, loading, select, table} from '../ui/components';
import {errorText, h, replace} from '../ui/dom';
import {messageDialog} from '../ui/modal';
import {toast} from '../ui/toast';

type Tab = 'runs' | 'files';

// Seçili iş ve sekme sayfalar arası geçişte korunur.
let jobFilter = '';
let tab: Tab = 'runs';

const runLimit = 200;

// showJobLogs, Loglar ekranını verilen işin çalışma geçmişiyle açar.
export async function showJobLogs(job: string): Promise<void> {
    const prev = [jobFilter, tab] as const;
    jobFilter = job;
    tab = 'runs';
    if (!(await navigate('logs'))) [jobFilter, tab] = prev;
}

export async function logsPage(el: HTMLElement): Promise<void> {
    const jobSelect = select([{value: '', label: t('gui.logs.allJobs')}], '', (v) => {
        jobFilter = v;
        void load();
    });
    const tabBar = h('div', {class: 'tabs', role: 'tablist'});
    const body = h('div');

    const renderTabs = () => replace(tabBar, ([
        ['runs', t('gui.logs.tabRuns')],
        ['files', t('gui.logs.tabFiles')],
    ] as Array<[Tab, string]>).map(([id, label]) => h('button', {
        type: 'button',
        role: 'tab',
        class: id === tab ? 'tab active' : 'tab',
        'aria-selected': String(id === tab),
        on: {
            click: () => {
                tab = id;
                renderTabs();
                void load();
            },
        },
    }, label)));

    let seq = 0;
    const load = async () => {
        const my = ++seq;
        replace(body, loading(t('gui.common.loading')));
        try {
            const view = tab === 'runs' ? runsTable(await ListRuns(jobFilter, runLimit)) : filesTable(await ListLogs(jobFilter));
            if (my === seq && el.isConnected) replace(body, view);
        } catch (err) {
            if (my === seq && el.isConnected) replace(body, alert('error', errorText(err)));
        }
    };

    el.append(
        pageHeader(t('gui.menu.logs'),
            h('div', {class: 'filter'}, jobSelect),
            button(t('gui.common.refresh'), {icon: 'refresh', variant: 'ghost', onClick: load})),
        h('div', {class: 'stack'}, tabBar, body));
    renderTabs();
    const loaded = load();

    // İş listesi filtre için; okunamazsa yalnızca "Tüm işler" kalır.
    try {
        const names = (await ListJobs()).jobs.map((j) => j.name);
        if (jobFilter && !names.some((n) => n.toLowerCase() === jobFilter.toLowerCase())) names.push(jobFilter);
        replace(jobSelect,
            h('option', {value: ''}, t('gui.logs.allJobs')),
            names.map((n) => h('option', {value: n, selected: n.toLowerCase() === jobFilter.toLowerCase()}, n)));
    } catch {
        // Filtre olmadan da çalışır.
    }
    await loaded;
}

function runsTable(runs: app.RunSummary[]): HTMLElement {
    return table<app.RunSummary>([
        {title: t('gui.logs.start'), render: (r) => fmtDate(r.start)},
        {title: t('gui.logs.job'), key: 'job'},
        {title: t('gui.logs.status'), render: (r) => r.readError ? badge(t('gui.logs.unreadable'), 'error') : exitBadge(r.exitCode, r.alreadyRunning)},
        {
            title: t('gui.logs.databases'), render: (r) => r.readError ? '' : h('span', {class: 'row'},
                r.succeeded > 0 && badge(`✓ ${r.succeeded}`, 'success'),
                r.failed > 0 && badge(`✗ ${r.failed}`, 'error'),
                r.skipped > 0 && badge(`− ${r.skipped}`, 'warning')),
        },
        {title: t('gui.logs.duration'), className: 'num', render: (r) => r.readError ? '' : fmtDuration(r.elapsedMs)},
        {title: t('gui.logs.error'), render: (r) => h('span', {class: 'ellipsis text-error', title: r.error || r.readError}, r.error || r.readError)},
    ], runs, {
        empty: t('gui.logs.noRuns'),
        onRowClick: openRun,
    });
}

// openRun ve openLog kaydı okuyup pencereyi açar; Promise okuma bitince
// çözülür (satır o sürede meşgul görünür), pencerenin kapanması beklenmez.
async function openRun(r: app.RunSummary): Promise<void> {
    if (r.readError) {
        void messageDialog(t('gui.logs.runTitle'), alert('error', r.readError));
        return;
    }
    try {
        const res = await ReadRun(r.path);
        void messageDialog(t('gui.logs.runTitle'), resultView(res), true);
    } catch (err) {
        toast(errorText(err), 'error');
    }
}

function filesTable(files: logging.LogInfo[]): HTMLElement {
    return table<logging.LogInfo>([
        {title: t('gui.logs.date'), render: (f) => fmtDay(f.date)},
        {title: t('gui.logs.job'), key: 'job'},
        {title: t('gui.logs.size'), className: 'num', render: (f) => fmtBytes(f.size)},
        {title: t('gui.logs.file'), render: (f) => h('span', {class: 'mono muted'}, f.path.split('\\').pop() ?? f.path)},
    ], files, {
        empty: t('gui.logs.noFiles'),
        onRowClick: openLog,
    });
}

async function openLog(f: logging.LogInfo): Promise<void> {
    try {
        const log = await ReadLog(f.path);
        const pre = h('pre', {class: 'log-view selectable'}, log.text);
        const view = h('div', {class: 'stack'},
            log.truncated && alert('info', t('gui.logs.truncated', {size: fmtBytes(log.size)})),
            pre);
        // Pencere açıldıktan sonra en sona kaydırılır (en yeni kayıtlar).
        requestAnimationFrame(() => pre.scrollTop = pre.scrollHeight);
        void messageDialog(t('gui.logs.logTitle', {job: f.job, date: fmtDay(f.date)}), view, true);
    } catch (err) {
        toast(errorText(err), 'error');
    }
}
