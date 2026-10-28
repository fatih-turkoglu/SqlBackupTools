// Birden fazla ekranda kullanılan, backend'e bağlı küçük parçalar
// (ui/components backend çağırmaz).
import {SelectFolder} from '../../wailsjs/go/app/App';
import {t} from '../i18n';
import {button, input} from '../ui/components';
import {Child, errorText, h} from '../ui/dom';
import {toast} from '../ui/toast';

// folderInput, metin kutusu + "Gözat" düğmesidir; seçilen klasör kutuya
// yazılır ve onChange çağrılır. extra, sağa eklenecek başka düğmelerdir.
export function folderInput(value: string, onChange: (v: string) => void, ...extra: Child[]): {
    el: HTMLElement;
    input: HTMLInputElement;
} {
    const inp = input({value, mono: true, onInput: onChange});
    const browse = button(t('gui.common.browse'), {
        icon: 'folder',
        onClick: async () => {
            try {
                const dir = await SelectFolder(t('gui.common.selectFolder'), inp.value.trim());
                if (dir) {
                    inp.value = dir;
                    onChange(dir);
                }
            } catch (err) {
                toast(errorText(err), 'error');
            }
        },
    });
    return {el: h('div', {class: 'input-row'}, inp, browse, extra), input: inp};
}
