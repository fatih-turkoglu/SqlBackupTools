// Yedek sekmesi: yedek klasörleri (sunucu yolu / erişim yolu), dosya adı
// şablonu ve BACKUP seçenekleri.
import {CheckFolderAccess, PreviewFileName} from '../../../wailsjs/go/app/App';
import {t} from '../../i18n';
import {alert, button, field, input, setFieldError, toggle} from '../../ui/components';
import {errorText, h, replace} from '../../ui/dom';
import {folderInput} from '../common';
import {Editor} from './editor';

export function backupTab(ed: Editor): HTMLElement {
    const s = ed.state;
    const b = s.job.backup;
    const accessResult = h('div');

    const defaultDir = s.conn?.server.defaultBackupDir ?? '';
    const server = folderInput(b.serverPath, (v) => {
        b.serverPath = v.trim();
        ed.changed();
    }, defaultDir && button(t('gui.backup.useServerDefault'), {
        title: defaultDir,
        onClick: () => {
            server.input.value = defaultDir;
            b.serverPath = defaultDir;
            ed.changed();
        },
    }));

    const checkAccess = async () => {
        const dir = (b.accessPath || b.serverPath).trim();
        if (!dir) {
            replace(accessResult, alert('warning', t('gui.backup.noFolder')));
            return;
        }
        try {
            await CheckFolderAccess(dir);
            if (ed.alive()) replace(accessResult, alert('success', t('gui.backup.accessOk', {path: dir})));
        } catch (err) {
            if (ed.alive()) replace(accessResult, alert('error', t('gui.backup.accessFailed', {path: dir, error: errorText(err)})));
        }
    };
    const access = folderInput(b.accessPath, (v) => {
        b.accessPath = v.trim();
        ed.changed();
    }, button(t('gui.backup.checkAccess'), {icon: 'check', onClick: checkAccess}));

    // Dosya adı önizlemesi (yazarken gecikmeli güncellenir).
    const preview = h('span', {class: 'mono selectable'});
    const templateInput = input({
        value: b.fileNameTemplate,
        mono: true,
        onInput: (v) => {
            b.fileNameTemplate = v;
            ed.changed();
            schedulePreview();
        },
    });
    const templateField = field(t('gui.backup.template'), templateInput, {hint: t('gui.backup.templateHint')});
    let timer = 0;
    const schedulePreview = () => {
        clearTimeout(timer);
        timer = window.setTimeout(updatePreview, 250);
    };
    const updatePreview = async () => {
        const db = s.job.databases[0] || s.databases?.find((d) => !d.isSystem)?.name || 'Database';
        try {
            const name = await PreviewFileName(s.job, db, s.conn?.server.serverName ?? '');
            if (!ed.alive()) return;
            preview.textContent = name;
            setFieldError(templateField, '');
        } catch (err) {
            if (!ed.alive()) return;
            preview.textContent = '';
            setFieldError(templateField, errorText(err));
        }
    };
    void updatePreview();

    const noCompression = s.conn && !s.conn.supportsCompression;

    return h('div', {class: 'stack'},
        h('section', {class: 'card'},
            h('h2', null, t('gui.backup.folders')),
            h('div', {class: 'stack'},
                field(t('gui.backup.serverPath'), server.el, {hint: t('gui.backup.serverPathHint'), name: 'backup.serverPath'}),
                field(t('gui.backup.accessPath'), access.el, {hint: t('gui.backup.accessPathHint')}),
                accessResult,
                alert('info', t('gui.backup.pathsInfo')))),

        h('section', {class: 'card'},
            h('h2', null, t('gui.backup.fileName')),
            h('div', {class: 'stack'},
                templateField,
                h('div', {class: 'row'}, h('span', {class: 'muted'}, t('gui.backup.preview')), preview))),

        h('section', {class: 'card'},
            h('h2', null, t('gui.backup.options')),
            h('div', {class: 'grid-2'},
                h('div', {class: 'stack-sm'},
                    toggle(t('gui.backup.compression'), b.compression, (v) => {
                        b.compression = v;
                        ed.changed();
                    }),
                    toggle(t('gui.backup.checksum'), b.checksum, (v) => {
                        b.checksum = v;
                        ed.changed();
                    }),
                    toggle(t('gui.backup.copyOnly'), b.copyOnly, (v) => {
                        b.copyOnly = v;
                        ed.changed();
                    }),
                    toggle(t('gui.backup.verify'), b.verify, (v) => {
                        b.verify = v;
                        ed.changed();
                    })),
                field(t('gui.backup.commandTimeout'), input({
                    type: 'number',
                    value: b.commandTimeoutMin,
                    min: 0,
                    onInput: (v) => {
                        b.commandTimeoutMin = parseInt(v, 10) || 0;
                        ed.changed();
                    },
                }), {hint: t('gui.backup.commandTimeoutHint'), name: 'backup.commandTimeoutMin'})),
            h('p', {class: 'field-hint'}, t('gui.backup.copyOnlyHint')),
            noCompression && alert('info', t('gui.backup.compressionUnsupported'))));
}
