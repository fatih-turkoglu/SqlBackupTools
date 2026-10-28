// İşler ekranı (Dashboard): iş kartları, son çalışma, sonraki çalışma ve
// iş eylemleri. Düzenleyici bu sayfanın alt görünümüdür; açıkken menüden
// başka sayfaya geçmek kaydedilmemiş değişiklik sorusunu tetikler.
import {CancelJob, DeleteJob, DuplicateJob, ListJobs, RunJobNow} from '../../wailsjs/go/app/App';
import {app} from '../../wailsjs/go/models';
import {exitBadge, fmtDate, scheduleText} from '../format';
import {t} from '../i18n';
import {pageHeader, PageHandle} from '../layout';
import {kv, onRunEvent, progressText, runningHere} from '../runs';
import {alert, badge, button, field, input, loading, progress, setFieldError} from '../ui/components';
import {errorText, h, replace} from '../ui/dom';
import {confirmDialog, openModal} from '../ui/modal';
import {toast} from '../ui/toast';
import {EditorState, isDirty, loadEditorState, renderEditor} from './editor/editor';
import {showJobLogs} from './logs';

// Liste görünürken çalışma durumları (zamanlanmış görevin başlattıkları da
// dahil) bu aralıkla yenilenir.
const refreshMs = 30000;

export function jobsPage(el: HTMLElement): PageHandle {
    // editorState doluysa düzenleyici açıktır.
    let editorState: EditorState | null = null;
    let editor: { dispose(): void } | null = null;
    let listVisible = false;
    // progressEls: arayüzden çalışan işlerin kartlarındaki ilerleme parçaları.
    let progressEls = new Map<string, { bar: ReturnType<typeof progress>; text: HTMLElement }>();
    let timer = 0;
    let loadSeq = 0;

    const confirmLeave = async (): Promise<boolean> => {
        if (!editorState || !isDirty(editorState)) return true;
        return confirmDialog(t('gui.editor.discardTitle'), t('gui.editor.discard'),
            {danger: true, confirmLabel: t('gui.editor.discardConfirm')});
    };

    const openEditor = async (name?: string) => {
        try {
            editorState = await loadEditorState(name);
        } catch (err) {
            toast(errorText(err), 'error');
            return;
        }
        showEditor();
    };

    const showEditor = () => {
        listVisible = false;
        clearInterval(timer);
        editor = renderEditor(el, editorState!, {
            close: async () => {
                if (!(await confirmLeave())) return;
                editor?.dispose();
                editor = null;
                editorState = null;
                showList();
            },
        });
    };

    const body = h('div', {class: 'stack'});

    const showList = () => {
        listVisible = true;
        replace(el,
            pageHeader(t('gui.menu.jobs'),
                button(t('gui.common.refresh'), {icon: 'refresh', variant: 'ghost', onClick: () => load()}),
                button(t('gui.jobs.new'), {icon: 'plus', variant: 'primary', onClick: () => openEditor()})),
            body);
        replace(body, loading(t('gui.common.loading')));
        void load();
        clearInterval(timer);
        timer = window.setInterval(() => {
            if (listVisible && !document.querySelector('dialog[open]')) void load();
        }, refreshMs);
    };

    const load = async () => {
        const seq = ++loadSeq;
        let list: app.JobList;
        try {
            list = await ListJobs();
        } catch (err) {
            if (listVisible && seq === loadSeq) replace(body, alert('error', errorText(err)));
            return;
        }
        if (!listVisible || seq !== loadSeq) return;
        progressEls = new Map();
        replace(body,
            list.errors?.length > 0 && alert('error', t('gui.jobs.unreadable'), list.errors),
            list.jobs.length === 0
                ? h('div', {class: 'card'}, h('div', {class: 'empty stack'},
                    h('p', null, t('gui.jobs.empty')),
                    h('div', null, button(t('gui.jobs.new'), {icon: 'plus', variant: 'primary', onClick: () => openEditor()}))))
                : h('div', {class: 'job-list'}, list.jobs.map(jobCard)));
    };

    const jobCard = (j: app.JobSummary): HTMLElement => {
        const here = runningHere(j.name);
        const running = j.running || here !== undefined;
        const task = j.task?.status;

        let progressArea: HTMLElement | false = false;
        if (running) {
            const bar = progress(here ? here.percent : null);
            const text = h('span', {class: 'muted'}, here !== undefined ? progressText(here) : t('gui.jobs.runningElsewhere'));
            if (here !== undefined) progressEls.set(j.name.toLowerCase(), {bar, text});
            progressArea = h('div', {class: 'stack-sm'}, bar.el, text);
        }

        let nextRun: string;
        if (!task?.exists) nextRun = t('gui.jobs.noTask');
        else if (!task.enabled) nextRun = t('gui.jobs.taskDisabled');
        else nextRun = fmtDate(task.nextRun) || '—';

        const last = j.lastRun;
        return h('section', {class: 'card job-card'},
            h('div', {class: 'card-head'},
                h('h2', null, j.name),
                running && badge(t('gui.jobs.running'), 'accent'),
                j.errors?.length > 0 && badge(t('gui.jobs.invalid'), 'error'),
                j.task?.outdated && badge(t('gui.jobs.taskOutdated'), 'warning')),
            kv([
                [t('gui.jobs.server'), j.server],
                [t('gui.jobs.databases'), j.includeNewDatabases
                    ? t('gui.jobs.allDatabases')
                    : h('span', {class: 'ellipsis', title: (j.databases ?? []).join(', ')},
                        t('gui.jobs.databaseCount', {count: j.databases?.length ?? 0}), ': ', (j.databases ?? []).join(', '))],
                [t('gui.jobs.schedule'), task?.exists ? scheduleText(j.schedule) : h('span', {class: 'muted'}, scheduleText(j.schedule))],
                [t('gui.jobs.lastRun'), last
                    ? [fmtDate(last.start), ' ', last.readError ? badge(t('gui.jobs.unreadableRun'), 'error') : exitBadge(last.exitCode, last.alreadyRunning)]
                    : h('span', {class: 'muted'}, t('gui.jobs.neverRun'))],
                [t('gui.jobs.nextRun'), nextRun],
            ]),
            j.errors?.length > 0 && alert('warning', t('gui.jobs.invalidInfo'), j.errors),
            progressArea,
            h('div', {class: 'row job-actions'},
                here !== undefined
                    ? button(t('gui.jobs.stop'), {icon: 'stop', variant: 'danger', onClick: () => stopJob(j.name)})
                    : button(t('gui.jobs.runNow'), {
                        icon: 'play',
                        variant: 'primary',
                        disabled: running || j.errors?.length > 0,
                        onClick: () => runJob(j.name),
                    }),
                button(t('gui.jobs.edit'), {icon: 'edit', onClick: () => openEditor(j.name)}),
                button(t('gui.jobs.duplicate'), {icon: 'copy', onClick: () => duplicateJob(j.name)}),
                button(t('gui.jobs.history'), {icon: 'history', onClick: () => showJobLogs(j.name)}),
                h('span', {class: 'spacer'}),
                button(t('gui.jobs.delete'), {icon: 'trash', variant: 'ghost', disabled: running, onClick: () => deleteJob(j.name)})));
    };

    const runJob = async (name: string) => {
        try {
            await RunJobNow(name);
        } catch (err) {
            toast(errorText(err), 'error');
        }
        await load();
    };

    const stopJob = async (name: string) => {
        if (!(await confirmDialog(t('gui.jobs.stopTitle'), t('gui.jobs.stopConfirm', {job: name}),
            {danger: true, confirmLabel: t('gui.jobs.stop')}))) return;
        if (!(await CancelJob(name))) toast(t('gui.jobs.stopNotHere'), 'warning');
    };

    const deleteJob = async (name: string) => {
        if (!(await confirmDialog(t('gui.jobs.deleteTitle'), t('gui.jobs.deleteConfirm', {job: name}),
            {danger: true, confirmLabel: t('gui.jobs.delete')}))) return;
        try {
            await DeleteJob(name);
            toast(t('gui.jobs.deleted', {job: name}), 'success');
        } catch (err) {
            toast(errorText(err), 'error');
        }
        await load();
    };

    const duplicateJob = async (name: string) => {
        const nameInput = input({value: `${name}_2`});
        const nameField = field(t('gui.jobs.duplicateName'), nameInput, {hint: t('gui.editor.nameHint')});
        const ok = await openModal<boolean>({
            title: t('gui.jobs.duplicateTitle', {job: name}),
            body: h('div', {class: 'stack'}, h('p', {class: 'muted'}, t('gui.jobs.duplicateInfo')), nameField),
            dismissValue: false,
            actions: [
                {label: t('gui.common.cancel'), value: false},
                {
                    label: t('gui.jobs.duplicate'), variant: 'primary', value: true,
                    run: async () => {
                        const res = await DuplicateJob(name, nameInput.value);
                        setFieldError(nameField, res.errors?.join('\n'));
                        return res.saved;
                    },
                },
            ],
        });
        if (ok) {
            toast(t('gui.jobs.duplicated', {job: nameInput.value.trim()}), 'success');
            await load();
        }
    };

    const unsubscribe = onRunEvent((ev) => {
        if (!listVisible) return;
        if (ev.type === 'progress') {
            // Kart henüz çizilmediyse 'started' olayının yüklemesi onu getirir.
            const p = progressEls.get(ev.job.toLowerCase());
            if (p) {
                p.bar.set(ev.progress.percent);
                p.text.textContent = progressText(ev.progress);
            }
            return;
        }
        void load();
    });

    showList();

    return {
        canLeave: confirmLeave,
        dispose() {
            unsubscribe();
            clearInterval(timer);
            listVisible = false;
            editor?.dispose();
        },
    };
}
