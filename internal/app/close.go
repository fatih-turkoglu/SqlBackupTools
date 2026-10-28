package app

import (
	"context"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sqlbackup/internal/i18n"
)

// SetUnsavedChanges, frontend'in kaydedilmemiş değişiklik olup olmadığını
// bildirmesidir (iş düzenleyici); pencere kapanırken sorulur.
func (a *App) SetUnsavedChanges(unsaved bool) {
	a.unsaved.Store(unsaved)
}

// BeforeClose, Wails OnBeforeClose işlevidir: kaydedilmemiş değişiklik veya
// bu arayüzden başlatılmış çalışan iş varsa kullanıcıya sorar; true dönerse
// pencere kapanmaz. Soru yerel ileti kutusuyla sorulur (frontend yanıt
// vermese de pencere kapatılabilsin diye).
func BeforeClose(a *App) func(context.Context) bool {
	return func(context.Context) bool {
		msg := a.closeWarning()
		return msg != "" && !a.confirm(i18n.T("gui.close.title"), msg)
	}
}

// closeWarning, kapanışta gösterilecek uyarıdır; boşsa sormadan kapanır.
func (a *App) closeWarning() string {
	var parts []string
	if a.unsaved.Load() {
		parts = append(parts, i18n.T("gui.close.unsaved"))
	}
	a.mu.Lock()
	names := make([]string, 0, len(a.running))
	for _, r := range a.running {
		names = append(names, r.name)
	}
	a.mu.Unlock()
	if len(names) > 0 {
		sort.Strings(names)
		parts = append(parts, i18n.T("gui.close.running", "jobs", strings.Join(names, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n") + "\n\n" + i18n.T("gui.close.confirm")
}

// confirmDialog, yerel Evet/Hayır ileti kutusudur (varsayılan Hayır).
func (a *App) confirmDialog(title, message string) bool {
	if a.ctx == nil {
		return true
	}
	res, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         title,
		Message:       message,
		DefaultButton: "No",
	})
	return err != nil || res == "Yes"
}
