package i18n

import "golang.org/x/sys/windows"

// windowsLanguages, kullanıcının Windows arayüz dillerini tercih sırasıyla
// döner (ör. "tr-TR", "en-US"). SYSTEM hesabında sistemin arayüz dilidir.
func windowsLanguages() []string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		return nil
	}
	return langs
}
