import './style.css';
import './app.css';

import {applyI18n, changeLanguage, languages, loadTranslations, onLanguageChange} from './i18n';

// Geçici başlangıç ekranı: asıl arayüz Faz 4'te yazılacak. Şimdilik çoklu
// dil altyapısını (dil seçimi + yeniden çizim) göstermek için kullanılır.
async function render(): Promise<void> {
    const state = await languages();
    const options = state.languages
        .map((l) => `<option value="${l.code}"${l.code === state.current ? ' selected' : ''}>${l.name}</option>`)
        .join('');
    document.querySelector('#app')!.innerHTML = `
        <h1 data-i18n="gui.app.title"></h1>
        <p data-i18n="gui.app.placeholder"></p>
        <label>
            <span data-i18n="gui.settings.language"></span>
            <select id="language">${options}</select>
        </label>
    `;
    applyI18n();
    document.getElementById('language')!.addEventListener('change', (ev) => {
        changeLanguage((ev.target as HTMLSelectElement).value).catch(console.error);
    });
}

onLanguageChange(() => {
    render().catch(console.error);
});

loadTranslations()
    .then(render)
    .catch(console.error);
