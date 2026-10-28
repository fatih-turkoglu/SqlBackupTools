// Bağlantı sekmesi: sunucu, kimlik doğrulama, şifreleme seçenekleri ve
// bağlantı testi.
import {TestConnection} from '../../../wailsjs/go/app/App';
import {app} from '../../../wailsjs/go/models';
import {t} from '../../i18n';
import {kv} from '../../runs';
import {alert, button, field, input, loading, select, toggle} from '../../ui/components';
import {errorText, h, replace} from '../../ui/dom';
import {Editor} from './editor';

export function connectionTab(ed: Editor): HTMLElement {
    const s = ed.state;
    const c = s.job.connection;
    const sqlAuth = c.auth === 'sql';
    const result = h('div');
    if (s.conn) replace(result, connectionInfo(s.conn));

    const password = input({
        type: 'password',
        value: s.passwordChanged ? s.password : '',
        placeholder: s.hasPassword && !s.passwordChanged ? t('gui.conn.passwordStored') : '',
        onInput: (v) => {
            s.password = v;
            s.passwordChanged = true;
            ed.changed();
        },
    });

    // Şifreleme kapalıyken sertifika zaten doğrulanmaz (mssql.ConnString).
    const trustCert = toggle(t('gui.conn.trustCert'), c.trustServerCert || !c.encrypt, (v) => {
        c.trustServerCert = v;
        ed.changed();
    });
    trustCert.querySelector('input')!.disabled = !c.encrypt;

    const test = async () => {
        replace(result, loading(t('gui.conn.testing')));
        try {
            const info = await TestConnection(ed.connectionRequest());
            s.conn = info;
            if (ed.alive()) replace(result, connectionInfo(info));
        } catch (err) {
            if (ed.alive()) replace(result, alert('error', errorText(err)));
        }
    };

    return h('div', {class: 'stack'},
        h('section', {class: 'card'},
            h('h2', null, t('gui.conn.server')),
            h('div', {class: 'grid-2'},
                field(t('gui.conn.serverName'), input({
                    value: c.server,
                    placeholder: t('gui.conn.serverPlaceholder'),
                    onInput: (v) => {
                        c.server = v;
                        ed.changed();
                    },
                }), {hint: t('gui.conn.serverHint')}),
                field(t('gui.conn.instance'), input({
                    value: c.instance,
                    placeholder: 'SQLEXPRESS',
                    onInput: (v) => {
                        c.instance = v.trim();
                        ed.changed();
                    },
                }), {hint: t('gui.conn.instanceHint')}),
                field(t('gui.conn.port'), input({
                    type: 'number',
                    value: c.port || '',
                    min: 0,
                    max: 65535,
                    onInput: (v) => {
                        c.port = parseInt(v, 10) || 0;
                        ed.changed();
                    },
                }), {hint: t('gui.conn.portHint')}),
                field(t('gui.conn.protocol'), select([
                    {value: '', label: t('gui.conn.protocolAuto')},
                    {value: 'tcp', label: 'TCP/IP'},
                    {value: 'np', label: 'Named Pipes'},
                    {value: 'lpc', label: 'Shared Memory'},
                ], c.protocol, (v) => {
                    c.protocol = v;
                    ed.changed();
                }), {hint: t('gui.conn.protocolHint')}))),

        h('section', {class: 'card'},
            h('h2', null, t('gui.conn.auth')),
            h('div', {class: 'grid-2'},
                field(t('gui.conn.authType'), select([
                    {value: 'sql', label: t('gui.conn.authSql')},
                    {value: 'windows', label: t('gui.conn.authWindows')},
                ], c.auth, (v) => {
                    c.auth = v;
                    ed.changed();
                    ed.rerenderTab();
                })),
                h('div'),
                sqlAuth && field(t('gui.conn.user'), input({
                    value: c.user,
                    onInput: (v) => {
                        c.user = v;
                        ed.changed();
                    },
                })),
                sqlAuth && field(t('gui.conn.password'), password,
                    {hint: s.hasPassword ? t('gui.conn.passwordHint') : ''})),
            sqlAuth && s.passwordError && !s.passwordChanged &&
                alert('warning', t('gui.conn.passwordUnreadable', {error: s.passwordError})),
            !sqlAuth && alert('info', t('gui.conn.windowsAuthInfo'))),

        h('section', {class: 'card'},
            h('h2', null, t('gui.conn.options')),
            h('div', {class: 'grid-2'},
                h('div', {class: 'stack-sm'},
                    toggle(t('gui.conn.encrypt'), c.encrypt, (v) => {
                        c.encrypt = v;
                        ed.changed();
                        ed.rerenderTab();
                    }),
                    trustCert,
                    h('p', {class: 'field-hint'}, t('gui.conn.trustCertHint'))),
                field(t('gui.conn.timeout'), input({
                    type: 'number',
                    value: c.timeoutSec,
                    min: 1,
                    onInput: (v) => {
                        c.timeoutSec = parseInt(v, 10) || 0;
                        ed.changed();
                    },
                })))),

        h('section', {class: 'card'},
            h('div', {class: 'card-head'},
                h('h2', null, t('gui.conn.test')),
                button(t('gui.conn.testButton'), {icon: 'plug', onClick: test})),
            result));
}

function connectionInfo(info: app.ConnectionInfo): HTMLElement {
    const srv = info.server;
    return h('div', {class: 'stack'},
        alert('success', t('gui.conn.testOk')),
        kv([
            [t('gui.conn.infoServer'), srv.serverName],
            [t('gui.conn.infoVersion'), [srv.productVersion, srv.productLevel].filter(Boolean).join(' · ')],
            [t('gui.conn.infoEdition'), srv.edition],
            [t('gui.conn.infoProtocol'), info.protocolLabel],
            [t('gui.conn.infoLogin'), srv.loginName],
            [t('gui.conn.infoSysAdmin'), srv.isSysAdmin ? t('gui.common.yes') : t('gui.common.no')],
            [t('gui.conn.infoBackupDir'), h('span', {class: 'mono selectable'}, srv.defaultBackupDir)],
        ]),
        !info.supportsCompression && alert('info', t('gui.conn.noCompression')));
}
