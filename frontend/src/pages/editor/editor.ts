// İş düzenleyici: İşler sayfasından açılan alt görünüm. Ayarlar sekmelere
// bölünür (Bağlantı, Veritabanları, Yedek, ZIP/Saklama, Zamanlama); hepsi tek
// bir EditorState üzerinde çalışır ve birlikte kaydedilir.
//
// Şifre frontend'e hiç gelmez: GetJob yalnızca kayıtlı olup olmadığını
// (hasPassword) bildirir; kullanıcı yeni şifre yazarsa passwordChanged ile
// SaveJob/TestConnection'a gönderilir.
//
// Doğrulama: kaydetme başarısız olursa hatalar alanların altında
// (field({name}) / errorSlot) ve sekme başlıklarında sayı olarak gösterilir;
// o andan itibaren her değişiklikte ValidateJob ile canlı güncellenir.
import {GetJob, NewJob, SaveJob, SetUnsavedChanges, ValidateJob} from '../../../wailsjs/go/app/App';
import {app, config, mssql} from '../../../wailsjs/go/models';
import {t} from '../../i18n';
import {pageHeader} from '../../layout';
import {alert, badge, button, field, input, setFieldErrors} from '../../ui/components';
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
    // Son kaydetme denemesinin (veya canlı doğrulamanın) hata ve uyarıları;
    // fieldErrors alan anahtarı (ör. "connection.server") → metinler.
    errors: string[];
    fieldErrors: Record<string, string[]>;
    warnings: string[];
    // validated: başarısız bir kaydetmeden sonra değişiklikler canlı doğrulanır.
    validated: boolean;
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
        fieldErrors: {},
        warnings: [],
        validated: false,
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

// tabOf, doğrulama alanının bulunduğu sekmedir (iş adı sekmelerin üstünde).
function tabOf(fieldKey: string): TabId | null {
    const prefix = fieldKey.split('.')[0];
    switch (prefix) {
        case 'connection':
        case 'databases':
        case 'backup':
        case 'schedule':
            return prefix;
        case 'archive':
        case 'retention':
            return 'archive';
    }
    return null;
}

function errorCount(s: EditorState, tab: TabId): number {
    return Object.entries(s.fieldErrors)
        .filter(([k]) => tabOf(k) === tab)
        .reduce((n, [, list]) => n + list.length, 0);
}

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
    // reportedDirty: backend'e (pencere kapanış uyarısı) son bildirilen durum.
    let reportedDirty = false;
    let validateTimer = 0;
    let validateSeq = 0;
    const title = h('span');
    const dirtyBadge = badge(t('gui.editor.unsaved'), 'warning');
    const alerts = h('div', {class: 'stack'});
    const tabBar = h('div', {class: 'tabs', role: 'tablist'});
    const tabBody = h('div', {class: 'tab-body'});

    const ed: Editor = {
        state: s,
        changed,
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
        const dirty = isDirty(s);
        dirtyBadge.hidden = !dirty;
        if (dirty !== reportedDirty) {
            reportedDirty = dirty;
            SetUnsavedChanges(dirty).catch(console.error);
        }
    }

    function changed(): void {
        updateHeader();
        if (!s.validated) return;
        clearTimeout(validateTimer);
        validateTimer = window.setTimeout(revalidate, 300);
    }

    // revalidate, kaydetmeden doğrular ve hataları günceller (canlı doğrulama).
    async function revalidate(): Promise<void> {
        const seq = ++validateSeq;
        try {
            const res = await ValidateJob(saveRequest());
            if (!mounted || seq !== validateSeq) return;
            s.errors = res.errors ?? [];
            s.fieldErrors = res.fieldErrors ?? {};
            renderErrors();
        } catch (err) {
            console.error(err);
        }
    }

    function renderAlerts(): void {
        replace(alerts,
            s.errors.length > 0 && alert('error', t('gui.editor.saveErrors'), s.errors),
            s.warnings.length > 0 && alert('warning', t('gui.editor.saveWarnings'), s.warnings));
    }

    // renderErrors, hata kutusunu, alan hatalarını ve sekme sayılarını günceller.
    function renderErrors(): void {
        renderAlerts();
        setFieldErrors(nameCard, s.fieldErrors);
        setFieldErrors(tabBody, s.fieldErrors);
        tabBar.querySelectorAll<HTMLButtonElement>('.tab').forEach((b) => {
            const n = errorCount(s, b.dataset.tab as TabId);
            b.classList.toggle('has-errors', n > 0);
            b.querySelector('.tab-errors')?.remove();
            if (n > 0) b.append(h('span', {class: 'tab-errors', title: t('gui.editor.tabErrors', {count: n})}, n));
        });
    }

    function saveRequest(): app.SaveJobRequest {
        return app.SaveJobRequest.createFrom({
            job: s.job,
            originalName: s.originalName,
            password: s.password,
            passwordChanged: s.passwordChanged,
        });
    }

    function renderTab(): void {
        if (!mounted) return;
        tabBar.querySelectorAll<HTMLButtonElement>('.tab').forEach((b) => {
            const active = b.dataset.tab === s.tab;
            b.classList.toggle('active', active);
            b.setAttribute('aria-selected', String(active));
        });
        replace(tabBody, tabs.find((x) => x.id === s.tab)!.render(ed));
        setFieldErrors(tabBody, s.fieldErrors);
    }

    async function save(): Promise<boolean> {
        let res: app.SaveJobResult;
        clearTimeout(validateTimer);
        ++validateSeq; // bekleyen canlı doğrulama sonucu bunun üzerine yazmasın
        try {
            res = await SaveJob(saveRequest());
        } catch (err) {
            toast(errorText(err), 'error');
            return false;
        }
        s.errors = res.errors ?? [];
        s.fieldErrors = res.fieldErrors ?? {};
        s.warnings = res.warnings ?? [];
        if (!res.saved) {
            s.validated = true;
            if (mounted) {
                // Açık sekmede hata yoksa hatalı ilk sekmeye geçilir.
                const first = tabs.find((tb) => errorCount(s, tb.id) > 0);
                if (first && errorCount(s, s.tab) === 0) {
                    s.tab = first.id;
                    renderTab();
                }
                renderErrors();
            }
            toast(t('gui.editor.saveFailed'), 'error');
            return false;
        }
        s.validated = false;
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
            updateHeader();
            renderTab();
            renderErrors();
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
            changed();
        },
    });
    const nameCard = h('section', {class: 'card'},
        field(t('gui.editor.name'), nameInput, {hint: t('gui.editor.nameHint'), name: 'name'}));

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
            nameCard,
            tabBar,
            tabBody));
    updateHeader();
    renderTab();
    renderErrors();
    if (!s.originalName) nameInput.focus();

    return {
        dispose() {
            mounted = false;
            clearTimeout(validateTimer);
            document.removeEventListener('keydown', onKey);
            if (reportedDirty) SetUnsavedChanges(false).catch(console.error);
        },
    };
}
