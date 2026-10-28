// Zamanlama sekmesi: zamanlama ayarları, Windows Görev Zamanlayıcı görevi ve
// .bat dosyası. Görev ve .bat kayıtlı iş dosyasını kullanır; bu yüzden
// işlemlerden önce iş kaydedilir (ensureSaved).
import {CreateTask, DeleteTask, GetTaskStatus, RunTask, SaveBatch, SelectBatchFile, TaskWarnings} from '../../../wailsjs/go/app/App';
import {app} from '../../../wailsjs/go/models';
import {dayName, exitBadge, fmtDate, toDate, weekDays} from '../../format';
import {t} from '../../i18n';
import {kv} from '../../runs';
import {alert, badge, button, checkbox, field, input, loading, select} from '../../ui/components';
import {errorText, h, replace} from '../../ui/dom';
import {confirmDialog} from '../../ui/modal';
import {toast} from '../../ui/toast';
import {Editor} from './editor';

export function scheduleTab(ed: Editor): HTMLElement {
    const s = ed.state;
    const sc = s.job.schedule;
    const warnings = h('div');
    const taskArea = h('div', {class: 'stack'});

    // Görev uyarıları (SYSTEM + Windows kimliği vb.) kaydedilmemiş ayarlarla
    // hesaplanır; değişiklikte gecikmeli güncellenir.
    let timer = 0;
    const refreshWarnings = async () => {
        try {
            const w = await TaskWarnings(s.job);
            if (ed.alive()) replace(warnings, w?.length > 0 && alert('warning', t('gui.schedule.warnings'), w));
        } catch {
            // Uyarılar yardımcı bilgidir; alınamazsa gösterilmez.
        }
    };
    const changed = () => {
        ed.changed();
        clearTimeout(timer);
        timer = window.setTimeout(refreshWarnings, 250);
    };

    const timeInput = h('input', {class: 'input', type: 'time', value: sc.time, required: true});
    timeInput.addEventListener('input', () => {
        sc.time = timeInput.value;
        changed();
    });

    const frequencyFields: HTMLElement[] = [];
    if (sc.frequency === 'weekly') {
        frequencyFields.push(field(t('gui.schedule.days'), h('div', {class: 'row days'},
            weekDays.map((d) => checkbox(dayName(d), sc.days.includes(d), (on) => {
                sc.days = weekDays.filter((x) => x === d ? on : sc.days.includes(x));
                changed();
            })))));
    }
    if (sc.frequency === 'hourly') {
        frequencyFields.push(field(t('gui.schedule.interval'), input({
            type: 'number',
            value: sc.intervalHours || '',
            min: 1,
            max: 23,
            onInput: (v) => {
                sc.intervalHours = parseInt(v, 10) || 0;
                changed();
            },
        }), {hint: t('gui.schedule.intervalHint')}));
    }

    const renderTask = (info: app.TaskInfo | null) => {
        if (!ed.alive()) return;
        if (!s.originalName) {
            replace(taskArea, alert('info', t('gui.task.notSaved')));
            return;
        }
        const st = info?.status;
        const exists = !!st?.exists;
        const action = (run: () => Promise<app.TaskActionResult>, done: string) => async () => {
            if (!(await ed.ensureSaved())) return;
            try {
                const res = await run();
                toast(done, 'success');
                renderTask(res.task);
            } catch (err) {
                toast(errorText(err), 'error');
            }
        };
        replace(taskArea,
            exists ? kv([
                [t('gui.task.path'), h('span', {class: 'mono selectable'}, st!.path)],
                [t('gui.task.state'), taskStateBadge(st!.state ?? '', st!.enabled)],
                [t('gui.task.lastRun'), toDate(st!.lastRun) ? [fmtDate(st!.lastRun), ' ', lastResult(st!.lastResult)] : t('gui.task.never')],
                [t('gui.task.nextRun'), fmtDate(st!.nextRun) || '—'],
                [t('gui.task.account'), st!.definition?.user || ''],
            ]) : h('p', {class: 'muted'}, t('gui.task.none')),
            exists && info!.outdated && alert('warning', t('gui.task.outdated')),
            h('div', {class: 'row'},
                button(exists ? t('gui.task.update') : t('gui.task.create'), {
                    variant: 'primary',
                    icon: 'check',
                    onClick: action(() => CreateTask(s.originalName), t('gui.task.created')),
                }),
                exists && button(t('gui.task.run'), {
                    icon: 'play',
                    onClick: action(() => RunTask(s.originalName), t('gui.task.started')),
                }),
                exists && button(t('gui.task.delete'), {
                    variant: 'danger',
                    icon: 'trash',
                    onClick: async () => {
                        if (!(await confirmDialog(t('gui.task.deleteTitle'), t('gui.task.deleteConfirm', {path: st!.path}),
                            {danger: true, confirmLabel: t('gui.task.delete')}))) return;
                        try {
                            const res = await DeleteTask(s.originalName);
                            toast(t('gui.task.deleted'), 'success');
                            renderTask(res.task);
                        } catch (err) {
                            toast(errorText(err), 'error');
                        }
                    },
                }),
                button(t('gui.common.refresh'), {icon: 'refresh', variant: 'ghost', onClick: loadTask})),
            h('p', {class: 'field-hint'}, t('gui.task.uacHint')));
    };
    const loadTask = async () => {
        if (!s.originalName) {
            renderTask(null);
            return;
        }
        replace(taskArea, loading(t('gui.common.loading')));
        try {
            renderTask(await GetTaskStatus(s.originalName));
        } catch (err) {
            if (ed.alive()) replace(taskArea, alert('error', errorText(err)));
        }
    };

    const saveBatch = async () => {
        if (!(await ed.ensureSaved())) return;
        try {
            const path = await SelectBatchFile(s.originalName);
            if (!path) return;
            const written = await SaveBatch(s.originalName, path);
            toast(t('gui.batch.saved', {path: written}), 'success');
        } catch (err) {
            toast(errorText(err), 'error');
        }
    };

    const root = h('div', {class: 'stack'},
        h('section', {class: 'card'},
            h('h2', null, t('gui.schedule.title')),
            h('div', {class: 'stack'},
                h('div', {class: 'grid-2'},
                    field(t('gui.schedule.frequency'), select([
                        {value: 'daily', label: t('gui.schedule.daily')},
                        {value: 'weekly', label: t('gui.schedule.weekly')},
                        {value: 'hourly', label: t('gui.schedule.hourly')},
                    ], sc.frequency, (v) => {
                        sc.frequency = v;
                        if (v === 'weekly' && sc.days.length === 0) sc.days = ['MON', 'TUE', 'WED', 'THU', 'FRI'];
                        if (v === 'hourly' && !sc.intervalHours) sc.intervalHours = 4;
                        changed();
                        ed.rerenderTab();
                    })),
                    field(sc.frequency === 'hourly' ? t('gui.schedule.startTime') : t('gui.schedule.time'), timeInput),
                    field(t('gui.schedule.runAs'), select([
                        {value: 'system', label: t('gui.schedule.runAsSystem')},
                        {value: 'current', label: t('gui.schedule.runAsCurrent')},
                    ], sc.runAs, (v) => {
                        sc.runAs = v;
                        changed();
                    }), {hint: t('gui.schedule.runAsHint')})),
                frequencyFields,
                warnings)),

        h('section', {class: 'card'},
            h('h2', null, t('gui.task.title')),
            taskArea),

        h('section', {class: 'card'},
            h('h2', null, t('gui.batch.title')),
            h('p', {class: 'field-hint'}, t('gui.batch.hint')),
            h('div', {class: 'row'}, button(t('gui.batch.save'), {icon: 'file', onClick: saveBatch}))));

    void refreshWarnings();
    void loadTask();
    return root;
}

function taskStateBadge(state: string, enabled: boolean): HTMLElement {
    if (!enabled || state === 'disabled') return badge(t('gui.task.stateDisabled'), 'warning');
    switch (state) {
        case 'running': return badge(t('gui.task.stateRunning'), 'accent');
        case 'ready': return badge(t('gui.task.stateReady'), 'success');
    }
    return badge(state || t('gui.task.stateUnknown'));
}

// lastResult, görevin son sonucudur: exit code sözleşmesindeki değerler
// rozet, diğerleri (Görev Zamanlayıcı HRESULT'ları) onaltılık gösterilir.
function lastResult(code: number): HTMLElement {
    if (code >= 0 && code <= 3) return exitBadge(code);
    return h('span', {class: 'mono'}, `0x${(code >>> 0).toString(16).toUpperCase()}`);
}
