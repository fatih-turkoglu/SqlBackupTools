// Arayüzden başlatılan çalışmaların durumu (backup:* olayları) ve çalışma
// sonucunun ayrıntı görünümü. Olaylar uygulama boyunca tek yerden dinlenir;
// iş bittiğinde hangi sayfa açık olursa olsun bildirim gösterilir.
import {backup} from '../wailsjs/go/models';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {dbStatusBadge, exitBadge, fmtBytes, fmtDate, fmtDuration} from './format';
import {t} from './i18n';
import {alert, badge, table} from './ui/components';
import {Child, h} from './ui/dom';
import {toast} from './ui/toast';

// Progress, backup.Progress'in karşılığıdır (yalnızca olaylarla geldiği için
// Wails modellerinde üretilmez).
export type Progress = {
    database: string;
    index: number;
    total: number;
    phase: 'backup' | 'verify' | 'zip';
    percent: number;
};

export type RunEvent =
    | { type: 'started'; job: string }
    | { type: 'progress'; job: string; progress: Progress }
    | { type: 'done'; job: string; result: backup.Result; warnings: string[] };

// active: bu arayüzde çalışan işler (küçük harfli ad → son ilerleme).
const active = new Map<string, Progress | null>();
const listeners = new Set<(ev: RunEvent) => void>();

export function initRuns(): void {
    EventsOn('backup:started', (d: { job: string }) => {
        active.set(d.job.toLowerCase(), null);
        emit({type: 'started', job: d.job});
    });
    EventsOn('backup:progress', (d: { job: string; progress: Progress }) => {
        active.set(d.job.toLowerCase(), d.progress);
        emit({type: 'progress', job: d.job, progress: d.progress});
    });
    EventsOn('backup:done', (d: { job: string; result: backup.Result; warnings: string[] | null }) => {
        active.delete(d.job.toLowerCase());
        notifyDone(d.job, d.result, d.warnings ?? []);
        emit({type: 'done', job: d.job, result: d.result, warnings: d.warnings ?? []});
    });
}

function emit(ev: RunEvent): void {
    listeners.forEach((fn) => fn(ev));
}

// onRunEvent, olaylara abone olur; dönen işlev aboneliği kaldırır.
export function onRunEvent(fn: (ev: RunEvent) => void): () => void {
    listeners.add(fn);
    return () => listeners.delete(fn);
}

// runningHere, iş bu arayüzden başlatılmış ve sürüyorsa son ilerlemeyi
// (henüz ilerleme yoksa null), çalışmıyorsa undefined döner.
export function runningHere(job: string): Progress | null | undefined {
    return active.get(job.toLowerCase());
}

function notifyDone(job: string, r: backup.Result, warnings: string[]): void {
    if (r.alreadyRunning) {
        toast(t('gui.run.doneAlreadyRunning', {job}), 'warning');
    } else if (r.exitCode === 0) {
        toast(t('gui.run.doneSuccess', {job}), 'success');
    } else if (r.exitCode === 1) {
        toast(t('gui.run.donePartial', {job}), 'warning');
    } else {
        toast(r.error ? t('gui.run.doneFailedError', {job, error: r.error}) : t('gui.run.doneFailed', {job}), 'error');
    }
    warnings.forEach((w) => toast(w, 'warning'));
}

// progressText, ilerleme satırının metnidir.
export function progressText(p: Progress | null): string {
    if (!p) return t('gui.run.starting');
    const args = {db: p.database, index: p.index, total: p.total, percent: p.percent};
    switch (p.phase) {
        case 'verify': return t('gui.run.phaseVerify', args);
        case 'zip': return t('gui.run.phaseZip', args);
    }
    return t('gui.run.phaseBackup', args);
}

// kv, etiket/değer çiftlerinden bilgi ızgarası üretir; boş değerler atlanır.
export function kv(pairs: Array<[string, Child]>): HTMLElement {
    return h('dl', {class: 'kv'}, pairs
        .filter(([, v]) => v !== '' && v !== null && v !== undefined && v !== false)
        .map(([k, v]) => [h('dt', null, k), h('dd', null, v)]));
}

// resultView, bir çalışma kaydının (backup.Result) ayrıntılarıdır.
export function resultView(r: backup.Result): HTMLElement {
    const [ok, failed, skipped] = counts(r);
    const parts: Child[] = [
        kv([
            [t('gui.run.job'), r.job],
            [t('gui.run.status'), exitBadge(r.exitCode, r.alreadyRunning)],
            [t('gui.run.server'), r.server],
            [t('gui.run.edition'), r.edition ?? ''],
            [t('gui.run.protocol'), r.protocol ?? ''],
            [t('gui.run.start'), fmtDate(r.start)],
            [t('gui.run.duration'), fmtDuration(r.elapsedMs)],
            [t('gui.run.counts'), t('gui.run.countsText', {ok, failed, skipped})],
        ]),
    ];
    if (r.error) parts.push(alert('error', r.error));
    if (r.warnings?.length) parts.push(alert('warning', t('gui.run.warnings'), r.warnings));

    if (r.databases?.length) {
        parts.push(h('h3', null, t('gui.run.databases')), table<backup.DBResult>([
            {title: t('gui.run.database'), key: 'database'},
            {title: t('gui.run.status'), render: (d) => dbStatusBadge(d.status)},
            {title: t('gui.run.file'), render: (d) => h('span', {class: 'mono selectable'}, d.file ?? '')},
            {title: t('gui.run.size'), className: 'num', render: (d) => d.fileBytes ? fmtBytes(d.fileBytes) : ''},
            {title: t('gui.run.verified'), className: 'shrink', render: (d) => d.verified ? badge(t('gui.common.yes'), 'success') : ''},
            {title: t('gui.run.duration'), className: 'num', render: (d) => fmtDuration(d.elapsedMs)},
            {title: t('gui.run.error'), render: (d) => d.error ? h('span', {class: 'text-error'}, d.error) : ''},
        ], r.databases));
    }
    if (r.archives?.length) {
        parts.push(h('h3', null, t('gui.run.archives')), table<backup.ArchiveResult>([
            {title: t('gui.run.file'), render: (a) => h('span', {class: 'mono selectable'}, a.file)},
            {title: t('gui.run.databases'), render: (a) => (a.databases ?? []).join(', ')},
            {title: t('gui.run.size'), className: 'num', render: (a) => a.sizeBytes ? fmtBytes(a.sizeBytes) : ''},
            {title: t('gui.run.bakDeleted'), className: 'shrink', render: (a) => a.bakDeleted ? badge(t('gui.common.yes'), 'success') : ''},
            {title: t('gui.run.error'), render: (a) => a.error ? h('span', {class: 'text-error'}, a.error) : ''},
        ], r.archives));
    }
    const ret = r.retention;
    if (ret && ret.mode !== 'none') {
        parts.push(h('h3', null, t('gui.run.retention')));
        if (ret.error) parts.push(alert('error', ret.error));
        parts.push(ret.deleted?.length
            ? table<backup.DeletedFile>([
                {title: t('gui.run.deletedFile'), render: (d) => h('span', {class: 'mono selectable'}, d.file)},
                {title: t('gui.run.size'), className: 'num', render: (d) => fmtBytes(d.bytes)},
                {title: t('gui.run.fileDate'), render: (d) => fmtDate(d.time)},
            ], ret.deleted)
            : h('p', {class: 'muted'}, t('gui.run.nothingDeleted')));
    }
    return h('div', {class: 'stack result-view'}, parts);
}

function counts(r: backup.Result): [number, number, number] {
    let ok = 0, failed = 0, skipped = 0;
    for (const d of r.databases ?? []) {
        if (d.status === 'success') ok++;
        else if (d.status === 'skipped') skipped++;
        else failed++;
    }
    return [ok, failed, skipped];
}
