package scheduler

import (
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"sqlbackup/internal/config"
	"sqlbackup/internal/i18n"
)

// TaskFolder, görevlerin Görev Zamanlayıcı'daki klasörüdür. Testler değiştirir.
var TaskFolder = `\SqlBackup`

// TaskPath, işin görev yoludur: \SqlBackup\<ad>.
func TaskPath(name string) string { return TaskFolder + `\` + name }

// systemSID, NT AUTHORITY\SYSTEM hesabıdır (yerelleştirilmiş addan bağımsız).
const systemSID = "S-1-5-18"

// Görev Zamanlayıcı şeması (Task Scheduler 1.2, Windows 7+). Yalnızca
// kullanılan öğeler tanımlıdır; okunurken bilinmeyenler yok sayılır.
type taskXML struct {
	XMLName      xml.Name     `xml:"http://schemas.microsoft.com/windows/2004/02/mit/task Task"`
	Version      string       `xml:"version,attr"`
	Registration registration `xml:"RegistrationInfo"`
	Triggers     triggers     `xml:"Triggers"`
	Principals   []principal  `xml:"Principals>Principal"`
	Settings     settings     `xml:"Settings"`
	Actions      actions      `xml:"Actions"`
}

type registration struct {
	Author      string `xml:"Author,omitempty"`
	Description string `xml:"Description,omitempty"`
	URI         string `xml:"URI,omitempty"`
}

type triggers struct {
	Calendar []calendarTrigger `xml:"CalendarTrigger"`
	Time     []timeTrigger     `xml:"TimeTrigger"`
}

type calendarTrigger struct {
	StartBoundary string        `xml:"StartBoundary"`
	Enabled       bool          `xml:"Enabled"`
	ByDay         *scheduleDay  `xml:"ScheduleByDay"`
	ByWeek        *scheduleWeek `xml:"ScheduleByWeek"`
}

type scheduleDay struct {
	DaysInterval int `xml:"DaysInterval"`
}

type scheduleWeek struct {
	Days          daysOfWeek `xml:"DaysOfWeek"`
	WeeksInterval int        `xml:"WeeksInterval"`
}

// daysOfWeek, <Monday/> gibi boş öğelerdir; işaretçi doluysa öğe yazılır.
type daysOfWeek struct {
	Monday    *struct{} `xml:"Monday"`
	Tuesday   *struct{} `xml:"Tuesday"`
	Wednesday *struct{} `xml:"Wednesday"`
	Thursday  *struct{} `xml:"Thursday"`
	Friday    *struct{} `xml:"Friday"`
	Saturday  *struct{} `xml:"Saturday"`
	Sunday    *struct{} `xml:"Sunday"`
}

type timeTrigger struct {
	StartBoundary string      `xml:"StartBoundary"`
	Enabled       bool        `xml:"Enabled"`
	Repetition    *repetition `xml:"Repetition"`
}

// repetition, Duration verilmediğinde süresiz tekrar eder.
type repetition struct {
	Interval          string `xml:"Interval"`
	StopAtDurationEnd bool   `xml:"StopAtDurationEnd"`
}

type principal struct {
	ID        string `xml:"id,attr"`
	UserID    string `xml:"UserId"`
	LogonType string `xml:"LogonType,omitempty"`
	RunLevel  string `xml:"RunLevel"`
}

type settings struct {
	MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
	DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
	StartWhenAvailable         bool   `xml:"StartWhenAvailable"`
	ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
	Enabled                    bool   `xml:"Enabled"`
}

type actions struct {
	Context string     `xml:"Context,attr"`
	Exec    []execItem `xml:"Exec"`
}

type execItem struct {
	Command          string `xml:"Command"`
	Arguments        string `xml:"Arguments,omitempty"`
	WorkingDirectory string `xml:"WorkingDirectory,omitempty"`
}

var weekdays = []struct {
	code string
	set  func(*daysOfWeek)
}{
	{"MON", func(d *daysOfWeek) { d.Monday = &struct{}{} }},
	{"TUE", func(d *daysOfWeek) { d.Tuesday = &struct{}{} }},
	{"WED", func(d *daysOfWeek) { d.Wednesday = &struct{}{} }},
	{"THU", func(d *daysOfWeek) { d.Thursday = &struct{}{} }},
	{"FRI", func(d *daysOfWeek) { d.Friday = &struct{}{} }},
	{"SAT", func(d *daysOfWeek) { d.Saturday = &struct{}{} }},
	{"SUN", func(d *daysOfWeek) { d.Sunday = &struct{}{} }},
}

// TaskOptions, görev tanımının işten gelmeyen parçalarıdır.
type TaskOptions struct {
	Command Command
	// User, "current" hesabıyla çalışan görevin kullanıcısıdır (DOMAIN\ad).
	User string
	// Now, başlangıç tarihidir (StartBoundary'nin gün kısmı). Saat işten gelir.
	Now time.Time
}

// TaskXML, işin zamanlamasından Görev Zamanlayıcı XML tanımını üretir.
//
//   - Günlük: her gün saat HH:MM. Haftalık: seçili günlerde HH:MM.
//     Saatlik: HH:MM'den başlayarak N saatte bir, süresiz.
//   - SYSTEM: en yüksek yetki, oturum açık olmasa da çalışır (oluşturmak
//     yönetici yetkisi ister). Geçerli kullanıcı: yalnızca kullanıcı oturum
//     açmışken, şifre saklanmadan (InteractiveToken), sınırlı yetkiyle.
//   - Kaçırılan çalışma bilgisayar açılınca yapılır; pilde de çalışır.
//     Görev süre sınırı yoktur (komut zaman aşımı işin ayarıdır); önceki
//     çalışma sürerken yenisi başlatılmaz (iş kilidi de ayrıca korur).
func TaskXML(j *config.Job, o TaskOptions) (string, error) {
	// Görev Zamanlayıcı eylemdeki %AD% kalıplarını ortam değişkeni olarak
	// genişletir ve kaçış yolu yoktur (.bat'taki %% gibi). Tek % zararsızdır.
	for _, p := range []string{o.Command.Exe, o.Command.JobFile, o.Command.Home} {
		if strings.Count(p, "%") > 1 {
			return "", i18n.Errorf("err.scheduler.percentPath", "path", p)
		}
	}
	args, err := o.Command.Args()
	if err != nil {
		return "", err
	}
	s := j.Schedule
	hm, err := time.ParseInLocation("15:04", s.Time, time.Local)
	if err != nil {
		return "", i18n.Errorf("err.config.scheduleTime")
	}
	start := time.Date(o.Now.Year(), o.Now.Month(), o.Now.Day(), hm.Hour(), hm.Minute(), 0, 0, time.Local).
		Format("2006-01-02T15:04:05")

	t := taskXML{
		Version: "1.2",
		Registration: registration{
			Author:      "SqlBackup",
			Description: i18n.T("common.taskDescription", "job", j.Name),
			URI:         TaskPath(j.Name),
		},
		Settings: settings{
			MultipleInstancesPolicy: "IgnoreNew",
			StartWhenAvailable:      true,
			ExecutionTimeLimit:      "PT0S",
			Enabled:                 true,
		},
		Actions: actions{Context: "Author", Exec: []execItem{{
			Command:          o.Command.Exe,
			Arguments:        args,
			WorkingDirectory: filepath.Dir(o.Command.Exe),
		}}},
	}

	switch s.Frequency {
	case config.FrequencyDaily:
		t.Triggers.Calendar = []calendarTrigger{{StartBoundary: start, Enabled: true, ByDay: &scheduleDay{DaysInterval: 1}}}
	case config.FrequencyWeekly:
		if len(s.Days) == 0 {
			return "", i18n.Errorf("err.config.scheduleNoDays")
		}
		w := &scheduleWeek{WeeksInterval: 1}
		for _, d := range s.Days {
			found := false
			for _, wd := range weekdays {
				if wd.code == d {
					wd.set(&w.Days)
					found = true
				}
			}
			if !found {
				return "", i18n.Errorf("err.config.scheduleDayInvalid", "value", d)
			}
		}
		t.Triggers.Calendar = []calendarTrigger{{StartBoundary: start, Enabled: true, ByWeek: w}}
	case config.FrequencyHourly:
		if s.IntervalHours < 1 || s.IntervalHours > 23 {
			return "", i18n.Errorf("err.config.scheduleInterval")
		}
		t.Triggers.Time = []timeTrigger{{StartBoundary: start, Enabled: true,
			Repetition: &repetition{Interval: fmt.Sprintf("PT%dH", s.IntervalHours)}}}
	default:
		return "", i18n.Errorf("err.config.scheduleFrequencyInvalid", "value", s.Frequency)
	}

	switch s.RunAs {
	case config.RunAsSystem:
		t.Principals = []principal{{ID: "Author", UserID: systemSID, RunLevel: "HighestAvailable"}}
	case config.RunAsCurrent:
		if strings.TrimSpace(o.User) == "" {
			return "", i18n.Errorf("err.scheduler.noUser")
		}
		t.Principals = []principal{{ID: "Author", UserID: o.User, LogonType: "InteractiveToken", RunLevel: "LeastPrivilege"}}
	default:
		return "", i18n.Errorf("err.config.runAsInvalid", "value", s.RunAs)
	}

	out, err := xml.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	// Metin COM'a UTF-16 BSTR olarak gider; bildirim buna uygun olmalı.
	return `<?xml version="1.0" encoding="UTF-16"?>` + "\n" + string(out), nil
}

// TaskDefinition, kayıtlı bir görevin XML'inden okunan özetidir.
type TaskDefinition struct {
	// RunAs, config.RunAsSystem veya config.RunAsCurrent; başka bir hesapsa boş.
	RunAs string `json:"runAs"`
	User  string `json:"user"`
	// Command ve Arguments, görevin ilk eylemidir.
	Command   string `json:"command"`
	Arguments string `json:"arguments"`
	// Schedule, tetikleyiciden geri okunan zamanlamadır (RunAs dahil).
	// Görev elle değiştirilmiş ve TaskXML biçimine uymuyorsa Frequency boştur.
	Schedule config.Schedule `json:"schedule"`
}

// Matches, kayıtlı görevin işin zamanlamasını, hesabını ve komutunu (c)
// yansıtıp yansıtmadığını döner. false ise görev yeniden oluşturulmalıdır
// (ör. iş kaydedildi ama görev güncellenmedi, program taşındı).
func (d TaskDefinition) Matches(j *config.Job, c Command) bool {
	want, got := j.Schedule, d.Schedule
	if got.RunAs != want.RunAs || got.Frequency != want.Frequency || got.Time != want.Time {
		return false
	}
	switch want.Frequency {
	case config.FrequencyWeekly:
		if !slices.Equal(sortedDays(got.Days), sortedDays(want.Days)) {
			return false
		}
	case config.FrequencyHourly:
		if got.IntervalHours != want.IntervalHours {
			return false
		}
	}
	args, err := c.Args()
	// Argümanlar yalnızca yol ve bayrak içerir; Windows yolları harf duyarsızdır.
	return err == nil && strings.EqualFold(d.Command, c.Exe) && strings.EqualFold(d.Arguments, args)
}

// sortedDays, gün kodlarını hafta sırasına dizer ve tekrarları atar.
func sortedDays(days []string) []string {
	out := []string{}
	for _, wd := range weekdays {
		if slices.Contains(days, wd.code) {
			out = append(out, wd.code)
		}
	}
	return out
}

// scheduleFromTriggers, TaskXML'in ürettiği tetikleyicilerden zamanlamayı
// geri okur; biçim tanınmazsa Frequency boş kalır.
func scheduleFromTriggers(tr triggers) config.Schedule {
	s := config.Schedule{Days: []string{}}
	hhmm := func(b string) string {
		if len(b) >= 16 && b[10] == 'T' {
			return b[11:16]
		}
		return ""
	}
	switch {
	case len(tr.Calendar) == 1 && len(tr.Time) == 0:
		c := tr.Calendar[0]
		s.Time = hhmm(c.StartBoundary)
		switch {
		case c.ByDay != nil && c.ByDay.DaysInterval == 1:
			s.Frequency = config.FrequencyDaily
		case c.ByWeek != nil && c.ByWeek.WeeksInterval <= 1:
			s.Frequency = config.FrequencyWeekly
			d := c.ByWeek.Days
			for _, p := range []struct {
				set  *struct{}
				code string
			}{{d.Monday, "MON"}, {d.Tuesday, "TUE"}, {d.Wednesday, "WED"}, {d.Thursday, "THU"},
				{d.Friday, "FRI"}, {d.Saturday, "SAT"}, {d.Sunday, "SUN"}} {
				if p.set != nil {
					s.Days = append(s.Days, p.code)
				}
			}
		}
	case len(tr.Time) == 1 && len(tr.Calendar) == 0 && tr.Time[0].Repetition != nil:
		var n int
		if _, err := fmt.Sscanf(tr.Time[0].Repetition.Interval, "PT%dH", &n); err == nil &&
			tr.Time[0].Repetition.Interval == fmt.Sprintf("PT%dH", n) {
			s.Frequency, s.IntervalHours, s.Time = config.FrequencyHourly, n, hhmm(tr.Time[0].StartBoundary)
		}
	}
	return s
}

// ParseTaskXML, görev XML'inden hesap, komut ve zamanlamayı okur.
func ParseTaskXML(s string) (TaskDefinition, error) {
	var t taskXML
	// encoding/xml UTF-16 bildirimini okuyamaz; metin zaten Go string'i (UTF-8).
	d := xml.NewDecoder(strings.NewReader(s))
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := d.Decode(&t); err != nil {
		return TaskDefinition{}, err
	}
	var def TaskDefinition
	if len(t.Principals) > 0 {
		p := t.Principals[0]
		def.User = p.UserID
		switch {
		case p.UserID == systemSID || strings.EqualFold(p.UserID, "SYSTEM") || strings.EqualFold(p.UserID, `NT AUTHORITY\SYSTEM`):
			def.RunAs = config.RunAsSystem
		case p.LogonType == "InteractiveToken":
			def.RunAs = config.RunAsCurrent
		}
	}
	if len(t.Actions.Exec) > 0 {
		def.Command, def.Arguments = t.Actions.Exec[0].Command, t.Actions.Exec[0].Arguments
	}
	def.Schedule = scheduleFromTriggers(t.Triggers)
	def.Schedule.RunAs = def.RunAs
	return def, nil
}
