// Veritabanları sekmesi: sunucudaki veritabanları listesi ve seçim.
//
// Seçim iki listede tutulur: databases (işaretli olanlar) ve
// excludedDatabases (listede görünüp işaretlenmeyenler). "Yeni veritabanlarını
// dahil et" açıkken çalışma anında sunucudaki tüm veritabanları,
// excludedDatabases hariç yedeklenir; bu yüzden liste yüklendiyse her
// değişiklikte iki liste birlikte güncellenir.
import {ListDatabases} from '../../../wailsjs/go/app/App';
import {mssql} from '../../../wailsjs/go/models';
import {fmtDate, fmtMB} from '../../format';
import {t} from '../../i18n';
import {alert, badge, button, loading, table, toggle} from '../../ui/components';
import {errorText, h, replace} from '../../ui/dom';
import {Editor} from './editor';

const lower = (s: string) => s.toLowerCase();

export function databasesTab(ed: Editor): HTMLElement {
    const s = ed.state;
    const j = s.job;
    const listArea = h('div', {class: 'stack'});
    const summary = h('span', {class: 'muted'});

    const has = (list: string[], name: string) => list.some((x) => lower(x) === lower(name));
    const disabled = (d: mssql.Database) => !d.canBackup || (d.isSystem && j.excludeSystemDatabases);
    // checked: "yeni DB'leri dahil et" açıkken hariç tutulmayanlar, kapalıyken seçilenler.
    const checked = (d: mssql.Database) => !disabled(d) &&
        (j.includeNewDatabases ? !has(j.excludedDatabases, d.name) : has(j.databases, d.name));

    // setChecked, işaretli adlardan iki listeyi yeniden kurar. Sunucuda
    // bulunmayan seçili adlar (liste dışı) korunur.
    const setChecked = (names: Set<string>) => {
        const list = s.databases ?? [];
        const listed = new Set(list.map((d) => lower(d.name)));
        j.databases = [
            ...j.databases.filter((n) => !listed.has(lower(n))),
            ...list.filter((d) => names.has(lower(d.name))).map((d) => d.name),
        ];
        j.excludedDatabases = [
            ...j.excludedDatabases.filter((n) => !listed.has(lower(n))),
            ...list.filter((d) => !names.has(lower(d.name))).map((d) => d.name),
        ];
        ed.changed();
    };
    const currentChecked = () => new Set((s.databases ?? []).filter(checked).map((d) => lower(d.name)));

    const updateSummary = () => {
        summary.textContent = j.includeNewDatabases
            ? t('gui.db.summaryAllNew', {count: j.excludedDatabases.length})
            : t('gui.db.summarySelected', {count: j.databases.length});
    };

    const renderList = () => {
        updateSummary();
        if (!s.databases) {
            replace(listArea,
                j.databases.length > 0 && !j.includeNewDatabases &&
                    h('p', null, t('gui.db.selectedList'), ' ', h('span', {class: 'mono'}, j.databases.join(', '))),
                h('div', {class: 'empty'}, t('gui.db.notLoaded')));
            return;
        }
        const list = s.databases;
        const missing = j.includeNewDatabases ? [] : j.databases.filter((n) => !list.some((d) => lower(d.name) === lower(n)));
        const enabled = list.filter((d) => !disabled(d));
        const allBox = h('input', {
            type: 'checkbox',
            checked: enabled.length > 0 && enabled.every(checked),
            indeterminate: enabled.some(checked) && !enabled.every(checked),
        });
        allBox.addEventListener('change', () => {
            const names = currentChecked();
            enabled.forEach((d) => allBox.checked ? names.add(lower(d.name)) : names.delete(lower(d.name)));
            setChecked(names);
            renderList();
        });

        replace(listArea,
            missing.length > 0 && h('div', {class: 'alert alert-warning'},
                t('gui.db.missing', {list: missing.join(', ')}), ' ',
                button(t('gui.db.removeMissing'), {
                    small: true,
                    onClick: () => {
                        j.databases = j.databases.filter((n) => !missing.includes(n));
                        ed.changed();
                        renderList();
                    },
                })),
            list.length > 0 && h('label', {class: 'check'}, allBox, h('span', null, t('gui.db.selectAll'))),
            table<mssql.Database>([
                {
                    title: '', className: 'shrink', render: (d) => {
                        const box = h('input', {type: 'checkbox', checked: checked(d), disabled: disabled(d)});
                        box.addEventListener('change', () => {
                            const names = currentChecked();
                            if (box.checked) names.add(lower(d.name));
                            else names.delete(lower(d.name));
                            setChecked(names);
                            renderList();
                        });
                        return h('label', {class: 'check'}, box);
                    },
                },
                {
                    title: t('gui.db.name'), render: (d) => h('span', {class: 'row'}, d.name,
                        d.isSystem && badge(t('gui.db.system')),
                        d.isReadOnly && badge(t('gui.db.readOnly'))),
                },
                {title: t('gui.db.state'), render: (d) => d.canBackup ? d.state : badge(d.state, 'warning')},
                {title: t('gui.db.recovery'), key: 'recoveryModel'},
                {title: t('gui.db.size'), className: 'num', render: (d) => fmtMB(d.sizeMB)},
                {title: t('gui.db.lastBackup'), render: (d) => fmtDate(d.lastFullBackup) || h('span', {class: 'muted'}, t('gui.db.never'))},
            ], list, {
                empty: t('gui.db.empty'),
                onRowClick: (d) => {
                    if (disabled(d)) return;
                    const names = currentChecked();
                    if (names.has(lower(d.name))) names.delete(lower(d.name));
                    else names.add(lower(d.name));
                    setChecked(names);
                    renderList();
                },
            }));
    };

    const load = async () => {
        replace(listArea, loading(t('gui.db.loading')));
        try {
            const list = await ListDatabases(ed.connectionRequest());
            s.databases = list ?? [];
        } catch (err) {
            if (ed.alive()) replace(listArea, alert('error', errorText(err)));
            return;
        }
        if (ed.alive()) renderList();
    };

    const root = h('div', {class: 'stack'},
        h('section', {class: 'card'},
            h('h2', null, t('gui.db.options')),
            h('div', {class: 'stack-sm'},
                toggle(t('gui.db.includeNew'), j.includeNewDatabases, (v) => {
                    // Görünen seçim korunur; yalnızca anlamı değişir.
                    const names = currentChecked();
                    j.includeNewDatabases = v;
                    if (s.databases) setChecked(names);
                    else ed.changed();
                    renderList();
                }),
                h('p', {class: 'field-hint'}, t('gui.db.includeNewHint')),
                toggle(t('gui.db.excludeSystem'), j.excludeSystemDatabases, (v) => {
                    j.excludeSystemDatabases = v;
                    if (s.databases) {
                        const names = currentChecked();
                        if (v) s.databases.filter((d) => d.isSystem).forEach((d) => names.delete(lower(d.name)));
                        setChecked(names);
                    } else {
                        ed.changed();
                    }
                    renderList();
                }))),
        h('section', {class: 'card'},
            h('div', {class: 'card-head'},
                h('h2', null, t('gui.db.list')),
                summary,
                button(t('gui.db.refresh'), {icon: 'refresh', onClick: load})),
            listArea));

    renderList();
    // Liste bu düzenleme oturumunda yüklenmediyse ve sunucu girildiyse otomatik yüklenir.
    if (!s.databases && j.connection.server.trim()) void load();
    return root;
}
