// ZIP ve Saklama sekmesi: yedek sonrası zip, zip sonrası .bak silme ve eski
// yedeklerin temizlenmesi (retention).
import {t} from '../../i18n';
import {alert, field, input, select, toggle} from '../../ui/components';
import {h} from '../../ui/dom';
import {Editor} from './editor';

export function archiveTab(ed: Editor): HTMLElement {
    const s = ed.state;
    const a = s.job.archive;
    const r = s.job.retention;

    const zipMode = select([
        {value: 'single', label: t('gui.archive.modeSingle')},
        {value: 'perDb', label: t('gui.archive.modePerDb')},
    ], a.zipPerDatabase ? 'perDb' : 'single', (v) => {
        a.zipPerDatabase = v === 'perDb';
        ed.changed();
    });
    zipMode.disabled = !a.zipEnabled;
    const deleteBak = toggle(t('gui.archive.deleteBak'), a.deleteBakAfterZip, (v) => {
        a.deleteBakAfterZip = v;
        ed.changed();
    });
    deleteBak.querySelector('input')!.disabled = !a.zipEnabled;

    let valueField: HTMLElement | false = false;
    if (r.mode === 'days' || r.mode === 'count') {
        valueField = field(r.mode === 'days' ? t('gui.archive.retentionDays') : t('gui.archive.retentionCount'), input({
            type: 'number',
            value: r.value || '',
            min: 1,
            onInput: (v) => {
                r.value = parseInt(v, 10) || 0;
                ed.changed();
            },
        }), {hint: r.mode === 'days' ? t('gui.archive.retentionDaysHint') : t('gui.archive.retentionCountHint')});
    }

    return h('div', {class: 'stack'},
        h('section', {class: 'card'},
            h('h2', null, t('gui.archive.zip')),
            h('div', {class: 'stack'},
                toggle(t('gui.archive.zipEnabled'), a.zipEnabled, (v) => {
                    a.zipEnabled = v;
                    if (!v) a.deleteBakAfterZip = false;
                    ed.changed();
                    ed.rerenderTab();
                }),
                h('div', {class: 'grid-2'}, field(t('gui.archive.mode'), zipMode)),
                deleteBak,
                h('p', {class: 'field-hint'}, t('gui.archive.zipHint')))),

        h('section', {class: 'card'},
            h('h2', null, t('gui.archive.retention')),
            h('div', {class: 'stack'},
                h('div', {class: 'grid-2'},
                    field(t('gui.archive.retentionMode'), select([
                        {value: 'none', label: t('gui.archive.retentionNone')},
                        {value: 'days', label: t('gui.archive.retentionByDays')},
                        {value: 'count', label: t('gui.archive.retentionByCount')},
                    ], r.mode, (v) => {
                        r.mode = v;
                        if (v === 'none') r.value = 0;
                        else if (r.value <= 0) r.value = v === 'days' ? 14 : 7;
                        ed.changed();
                        ed.rerenderTab();
                    })),
                    valueField),
                r.mode !== 'none' && alert('info', t('gui.archive.retentionInfo')))),

        (a.zipEnabled || r.mode !== 'none') && alert('warning', t('gui.archive.accessWarning')));
}
