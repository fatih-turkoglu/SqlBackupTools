// İş düzenleyici: İşler sayfasından açılan alt görünüm. Ayarlar sekmelere
// bölünür (Bağlantı, Veritabanları, Yedek, ZIP/Saklama, Zamanlama); hepsi tek
// bir EditorState üzerinde çalışır ve birlikte kaydedilir.
//
// Şifre frontend'e hiç gelmez: GetJob yalnızca kayıtlı olup olmadığını
// (hasPassword) bildirir; kullanıcı yeni şifre yazarsa passwordChanged ile
// SaveJob/TestConnection'a gönderilir.
import {GetJob, NewJob, SaveJob} from '../../../wailsjs/go/app/App';
import {app, config, mssql} from '../../../wailsjs/go/models';
import {t} from '../../i18n';
import {pageHeader} from '../../layout';
import {alert, badge, button, field, input} from '../../ui/components';
import {errorText, h, replace} from '../../ui/dom';
import {confirmDialog} from '../../ui/modal';
import {toast} from '../../ui/toast';
import {archiveTab} from './archive';
import {backupTab} from './backup';
import {connectionTab} from './connection';
import {databasesTab} from './databases';
import {scheduleTab} from './schedule';

export type TabId = 'connection' | 'databases' | 'backup' | 'archive' | 'schedule';

export type EditorState = {
    job: config.Job;
    // originalName, işin kayıtlı adıdır; yeni (hiç kaydedilmemiş) işte boş.
    originalName: string;
    password: string;
    passwordChanged: boolean;
    hasPassword: boolean;
    passwordError: string;
    // snapshot, son kaydedilen/yüklenen hâldir (kaydedilmemiş değişiklik kontrolü).
    snapshot: string;
    tab: TabId;
    // Bu düzenleme oturumunda sunucudan alınan bilgiler (sekmeler arası paylaşılır).
    conn?: app.ConnectionInfo;
    databases?: mssql.Database[];
    // Son kaydetme denemesinin hata ve uyarıları.
    errors: string[];
    warnings: string[];
};

// Editor, sekmelerin düzenleyiciyle konuştuğu arayüzdür.
export type Editor = {
    state: EditorState;
    // changed, bir alan değiştiğinde çağrılır (kaydedilmemiş işaretini günceller).
    changed(): void;
    // rerenderTab, açık sekmeyi durumdan yeniden çizer (görünürlüğü değişen alanlar için).
    rerenderTab(): void;
    connectionRequest(): app.ConnectionRequest;
    save(): Promise<boolean>;
    // ensureSaved, iş kayıtlı ve değişmemişse true; değilse kaydetmeyi önerir.
    ensureSaved(): Promise<boolean>;
    // alive, düzenleyici hâlâ ekranda mı (eşzamansız sonuçları yazmadan önce).
    alive(): boolean;
};

function snapshotOf(s: EditorState): string {
    return JSON.stringify({job: s.job, password: s.passwordChanged ? s.password : null});
}

export function isDirty(s: EditorState): boolean {
    return snapshotOf(s) !== s.snapshot;
}

// loadEditorState, kayıtlı işi (name) veya yeni işi düzenleme için hazırlar.
export async function loadEditorState(name?: string): Promise<EditorState> {
    const s: EditorState = {
        job: null as unknown as config.Job,
        originalName: '',
        password: '',
        passwordChanged: false,
        hasPassword: false,
        passwordError: '',
        snapshot: '',
        tab: 'connection',
        errors: [],
        warnings: [],
    };
    if (name) {
        const d = await GetJob(name);
        s.job = d.job!;
        s.originalName = d.job!.name;
        s.hasPassword = d.hasPassword;
        s.passwordError = d.passwordError;
    } else {
        s.job = await NewJob();
    }
    s.job.databases ??= [];
    s.job.excludedDatabases ??= [];
    s.job.schedule.days ??= [];
    s.snapshot = snapshotOf(s);
    return s;
}

export type EditorOptions = {
    // close, kullanıcı listeye dönmek istediğinde çağrılır (kaydedilmemiş
    // değişiklik sorusunu çağıran sorar).
    close: () => void;
};

const tabs: Array<{ id: TabId; label: () => string; render: (ed: Editor) => HTMLElement }> = [
    {id: 'connection', label: () => t('gui.editor.tabConnection'), render: connectionTab},
    {id: 'databases', label: () => t('gui.editor.tabDatabases'), render: databasesTab},
    {id: 'backup', label: () => t('gui.editor.tabBackup'), render: backupTab},
    {id: 'archive', label: () => t('gui.editor.tabArchive'), render: archiveTab},
    {id: 'schedule', label: () => t('gui.editor.tabSchedule'), render: scheduleTab},
];

// renderEditor, düzenleyiciyi el içine çizer; dönen dispose ekrandan
// kaldırırken çağrılmalıdır.
export function renderEditor(el: HTMLElement, s: EditorState, opts: EditorOptions): { dispose(): void } {
    let mounted = true;
    const title = h('span');
    const dirtyBadge = badge(t('gui.editor.unsaved'), 'warning');
    const alerts = h('div', {class: 'stack'});
    const tabBar = h('div', {class: 'tabs', role: 'tablist'});
    const tabBody = h('div', {class: 'tab-body'});

    const ed: Editor = {
        state: s,
        changed: updateHeader,
        rerenderTab: renderTab,
        connectionRequest: () => app.ConnectionRequest.createFrom({
            connection: s.job.connection,
            password: s.password,
            passwordChanged: s.passwordChanged,
            storedJob: s.originalName,
        }),
        save,
        ensureSaved,
        alive: () => mounted,
    };

    function updateHeader(): void {
        title.textContent = s.originalName ? s.originalName : t('gui.editor.newJob');
        dirtyBadge.hidden = !isDirty(s);
    }

    function renderAlerts(): void {
        replace(alerts,
            s.errors.length > 0 && alert('error', t('gui.editor.saveErrors'), s.errors),
            s.warnings.length > 0 && alert('warning', t('gui.editor.saveWarnings'), s.warnings));
    }

    function renderTab(): void {
        if (!mounted) return;
        tabBar.querySelectorAll<HTMLButtonElement>('.tab').forEach((b) => {
            const active = b.dataset.tab === s.tab;
            b.classList.toggle('active', active);
            b.setAttribute('aria-selected', String(active));
        });
        replace(tabBody, tabs.find((x) => x.id === s.tab)!.render(ed));
    }

    async function save(): Promise<boolean> {
        let res: app.SaveJobResult;
        try {
            res = await SaveJob(app.SaveJobRequest.createFrom({
                job: s.job,
                originalName: s.originalName,
                password: s.password,
                passwordChanged: s.passwordChanged,
            }));
        } catch (err) {
            toast(errorText(err), 'error');
            return false;
        }
        s.errors = res.errors ?? [];
        s.warnings = res.warnings ?? [];
        if (!res.saved) {
            if (mounted) renderAlerts();
            toast(t('gui.editor.saveFailed'), 'error');
            return false;
        }
        s.job.name = s.job.name.trim();
        s.originalName = s.job.name;
        if (s.job.connection.auth !== 'sql') {
            s.hasPassword = false;
        } else if (s.passwordChanged) {
            s.hasPassword = s.password !== '';
            s.passwordError = '';
        }
        s.password = '';
        s.passwordChanged = false;
        s.snapshot = snapshotOf(s);
        toast(t('gui.editor.saved', {job: s.job.name}), 'success');
        if (mounted) {
            renderAlerts();
            updateHeader();
            renderTab();
        }
        return true;
    }

    async function ensureSaved(): Promise<boolean> {
        if (s.originalName && !isDirty(s)) return true;
        const ok = await confirmDialog(t('gui.editor.saveFirstTitle'), t('gui.editor.saveFirst'),
            {confirmLabel: t('gui.editor.save')});
        return ok && save();
    }

    const nameInput = input({
        value: s.job.name,
        placeholder: t('gui.editor.namePlaceholder'),
        onInput: (v) => {
            s.job.name = v;
            updateHeader();
        },
    });

    // Ctrl+S kaydeder.
    const onKey = (ev: KeyboardEvent) => {
        if (ev.ctrlKey && !ev.altKey && ev.key.toLowerCase() === 's') {
            ev.preventDefault();
            if (!document.querySelector('dialog[open]')) saveBtn.click();
        }
    };
    document.addEventListener('keydown', onKey);

    const saveBtn = button(t('gui.editor.save'), {variant: 'primary', icon: 'check', onClick: save});
    const header = pageHeader(h('span', {class: 'row'}, title, dirtyBadge),
        button(t('gui.editor.back'), {variant: 'ghost', icon: 'back', onClick: () => opts.close()}),
        saveBtn);

    replace(tabBar, tabs.map((tb) => h('button', {
        type: 'button',
        class: 'tab',
        role: 'tab',
        dataset: {tab: tb.id},
        on: {
            click: () => {
                s.tab = tb.id;
                renderTab();
            },
        },
    }, tb.label())));

    replace(el,
        header,
        h('div', {class: 'stack'},
            alerts,
            h('section', {class: 'card'},
                field(t('gui.editor.name'), nameInput, {hint: t('gui.editor.nameHint')})),
            tabBar,
            tabBody));
    updateHeader();
    renderAlerts();
    renderTab();
    if (!s.originalName) nameInput.focus();

    return {
        dispose() {
            mounted = false;
            document.removeEventListener('keydown', onKey);
        },
    };
}
