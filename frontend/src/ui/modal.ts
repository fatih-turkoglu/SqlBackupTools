// Modal pencereler (<dialog>). Esc ile kapanır, odak modal içinde kalır.
// Düğme metinleri çağırandan gelir; varsayılanlar gui.common.* anahtarlarıdır.
import {t} from '../i18n';
import {button, ButtonOptions} from './components';
import {Child, errorText, h} from './dom';
import {toast} from './toast';

export type ModalAction<T> = {
    label: string;
    variant?: ButtonOptions['variant'];
    // value, modal bu düğmeyle kapanınca Promise'in değeridir.
    value: T;
    // run verilirse önce çalışır (buton meşgul görünür); false dönerse modal
    // açık kalır (ör. form doğrulama), hata fırlatırsa hata toast'la gösterilir
    // ve modal açık kalır.
    run?: () => Promise<boolean | void> | boolean | void;
};

export type ModalOptions<T> = {
    title: string;
    body: Child;
    actions: ModalAction<T>[];
    // dismissValue, Esc ile kapatıldığında dönen değerdir.
    dismissValue: T;
    wide?: boolean;
};

export function openModal<T>(opts: ModalOptions<T>): Promise<T> {
    return new Promise<T>((resolve) => {
        const dialog = h('dialog', {class: opts.wide ? 'modal wide' : 'modal'});
        let result = opts.dismissValue;
        const footer = h('div', {class: 'modal-footer'}, opts.actions.map((a) => button(a.label, {
            variant: a.variant,
            onClick: async () => {
                try {
                    if (a.run && (await a.run()) === false) return;
                } catch (err) {
                    toast(errorText(err), 'error');
                    return;
                }
                result = a.value;
                dialog.close();
            },
        })));
        dialog.append(
            h('div', {class: 'modal-header'}, h('h2', null, opts.title)),
            h('div', {class: 'modal-body'}, opts.body),
            footer,
        );
        dialog.addEventListener('close', () => {
            dialog.remove();
            resolve(result);
        });
        document.body.appendChild(dialog);
        dialog.showModal();
        // Varsayılan odak birincil düğmede (yoksa sonuncusunda).
        const primary = footer.querySelector<HTMLButtonElement>('.btn-primary, .btn-danger') ??
            footer.querySelector<HTMLButtonElement>('button:last-child');
        primary?.focus();
    });
}

export type ConfirmOptions = {
    confirmLabel?: string;
    danger?: boolean;
};

// confirmDialog, Evet/İptal sorusu sorar; onaylanırsa true döner.
export function confirmDialog(title: string, message: Child, opts: ConfirmOptions = {}): Promise<boolean> {
    return openModal<boolean>({
        title,
        body: message,
        dismissValue: false,
        actions: [
            {label: t('gui.common.cancel'), value: false},
            {label: opts.confirmLabel ?? t('gui.common.ok'), variant: opts.danger ? 'danger' : 'primary', value: true},
        ],
    });
}

// messageDialog, yalnızca Tamam düğmeli bilgi penceresidir (ör. hata listesi).
export function messageDialog(title: string, message: Child, wide = false): Promise<void> {
    return openModal<void>({
        title,
        body: message,
        wide,
        dismissValue: undefined,
        actions: [{label: t('gui.common.ok'), variant: 'primary', value: undefined}],
    });
}
