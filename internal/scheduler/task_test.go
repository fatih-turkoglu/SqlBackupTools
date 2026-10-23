package scheduler

import (
	"strings"
	"testing"
	"time"

	"sqlbackup/internal/config"
)

var testNow = time.Date(2026, 10, 23, 15, 30, 0, 0, time.Local)

func testCommand() Command {
	return Command{Exe: `C:\Program Files\SqlBackup\SqlBackup.exe`, JobFile: `C:\ProgramData\SqlBackup\jobs\gece & <test>.json`}
}

func scheduleJob(edit func(*config.Schedule)) *config.Job {
	j := config.NewJob("gece")
	if edit != nil {
		edit(&j.Schedule)
	}
	return j
}

func TestTaskXMLDaily(t *testing.T) {
	x, err := TaskXML(scheduleJob(nil), TaskOptions{Command: testCommand(), Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-16"?>`,
		`<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task" version="1.2">`,
		`<URI>\SqlBackup\gece</URI>`,
		`<StartBoundary>2026-10-23T02:00:00</StartBoundary>`,
		`<ScheduleByDay>`, `<DaysInterval>1</DaysInterval>`,
		`<UserId>S-1-5-18</UserId>`, `<RunLevel>HighestAvailable</RunLevel>`,
		`<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>`,
		`<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>`,
		`<StartWhenAvailable>true</StartWhenAvailable>`,
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`<Command>C:\Program Files\SqlBackup\SqlBackup.exe</Command>`,
		// XML kaçışları: & < > " doğru yazılmalı.
		`<Arguments>run --job &#34;C:\ProgramData\SqlBackup\jobs\gece &amp; &lt;test&gt;.json&#34;</Arguments>`,
		`<WorkingDirectory>C:\Program Files\SqlBackup</WorkingDirectory>`,
		`gece`, // açıklama
	} {
		if !strings.Contains(x, want) {
			t.Errorf("XML'de yok: %s\n%s", want, x)
		}
	}
	if strings.Contains(x, "LogonType") || strings.Contains(x, "ScheduleByWeek") || strings.Contains(x, "TimeTrigger") {
		t.Errorf("gereksiz öğe:\n%s", x)
	}
}

func TestTaskXMLWeeklyCurrentUser(t *testing.T) {
	j := scheduleJob(func(s *config.Schedule) {
		s.Frequency, s.Time, s.Days, s.RunAs = config.FrequencyWeekly, "23:45", []string{"MON", "FRI", "SUN"}, config.RunAsCurrent
	})
	x, err := TaskXML(j, TaskOptions{Command: testCommand(), User: `PC01\Fatih TÜRKOĞLU`, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<StartBoundary>2026-10-23T23:45:00</StartBoundary>`,
		`<Monday></Monday>`, `<Friday></Friday>`, `<Sunday></Sunday>`, `<WeeksInterval>1</WeeksInterval>`,
		`<UserId>PC01\Fatih TÜRKOĞLU</UserId>`, `<LogonType>InteractiveToken</LogonType>`, `<RunLevel>LeastPrivilege</RunLevel>`,
	} {
		if !strings.Contains(x, want) {
			t.Errorf("XML'de yok: %s\n%s", want, x)
		}
	}
	for _, no := range []string{"Tuesday", "Wednesday", "Thursday", "Saturday", "S-1-5-18"} {
		if strings.Contains(x, no) {
			t.Errorf("XML'de olmamalı: %s", no)
		}
	}
}

func TestTaskXMLHourly(t *testing.T) {
	j := scheduleJob(func(s *config.Schedule) { s.Frequency, s.Time, s.IntervalHours = config.FrequencyHourly, "00:15", 6 })
	x, err := TaskXML(j, TaskOptions{Command: testCommand(), Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<TimeTrigger>`, `<StartBoundary>2026-10-23T00:15:00</StartBoundary>`,
		`<Interval>PT6H</Interval>`, `<StopAtDurationEnd>false</StopAtDurationEnd>`} {
		if !strings.Contains(x, want) {
			t.Errorf("XML'de yok: %s\n%s", want, x)
		}
	}
	// Duration verilmez → süresiz tekrar.
	if strings.Contains(x, "<Duration>") {
		t.Errorf("Duration olmamalı:\n%s", x)
	}
}

func TestTaskXMLErrors(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Schedule)
		user string
		want string
	}{
		{"saat", func(s *config.Schedule) { s.Time = "25:00" }, "", "SS:DD"},
		{"gün yok", func(s *config.Schedule) { s.Frequency, s.Days = config.FrequencyWeekly, nil }, "", "en az bir gün"},
		{"gün", func(s *config.Schedule) { s.Frequency, s.Days = config.FrequencyWeekly, []string{"PZT"} }, "", `"PZT"`},
		{"aralık", func(s *config.Schedule) { s.Frequency, s.IntervalHours = config.FrequencyHourly, 0 }, "", "1-23"},
		{"sıklık", func(s *config.Schedule) { s.Frequency = "monthly" }, "", `"monthly"`},
		{"hesap", func(s *config.Schedule) { s.RunAs = "admin" }, "", `"admin"`},
		{"kullanıcı", func(s *config.Schedule) { s.RunAs = config.RunAsCurrent }, " ", "kullanıcı hesabı"},
	}
	for _, tt := range tests {
		_, err := TaskXML(scheduleJob(tt.edit), TaskOptions{Command: testCommand(), User: tt.user, Now: testNow})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v (beklenen %q)", tt.name, err, tt.want)
		}
	}
	bad := testCommand()
	bad.JobFile = ""
	if _, err := TaskXML(scheduleJob(nil), TaskOptions{Command: bad, Now: testNow}); err == nil {
		t.Error("boş iş yolu kabul edilmemeli")
	}
	// %AD% Görev Zamanlayıcı'da genişletilir; tek % sorun değil.
	bad = testCommand()
	bad.Exe = `C:\Araçlar %USERNAME%\SqlBackup.exe`
	if _, err := TaskXML(scheduleJob(nil), TaskOptions{Command: bad, Now: testNow}); err == nil ||
		!strings.Contains(err.Error(), "ortam değişkeni") {
		t.Errorf("%%AD%% içeren yol: %v", err)
	}
	ok := testCommand()
	ok.Home = `D:\Yedek %100`
	if _, err := TaskXML(scheduleJob(nil), TaskOptions{Command: ok, Now: testNow}); err != nil {
		t.Errorf("tek %% kabul edilmeli: %v", err)
	}
}

func TestParseTaskXML(t *testing.T) {
	for _, runAs := range []string{config.RunAsSystem, config.RunAsCurrent} {
		j := scheduleJob(func(s *config.Schedule) { s.RunAs = runAs })
		x, err := TaskXML(j, TaskOptions{Command: testCommand(), User: `PC01\Fatih`, Now: testNow})
		if err != nil {
			t.Fatal(err)
		}
		def, err := ParseTaskXML(x)
		if err != nil {
			t.Fatal(err)
		}
		args, _ := testCommand().Args()
		if def.RunAs != runAs || def.Command != testCommand().Exe || def.Arguments != args {
			t.Errorf("%s: %+v", runAs, def)
		}
	}
	// Görev Zamanlayıcı'nın döndürdüğü biçim (SYSTEM adıyla, fazladan öğelerle).
	def, err := ParseTaskXML(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Principals><Principal id="Author"><UserId>NT AUTHORITY\SYSTEM</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Settings><Priority>7</Priority></Settings>
  <Actions Context="Author"><Exec><Command>D:\Araçlar\SqlBackup.exe</Command></Exec></Actions>
</Task>`)
	if err != nil || def.RunAs != config.RunAsSystem || def.Command != `D:\Araçlar\SqlBackup.exe` {
		t.Errorf("%+v %v", def, err)
	}
}

func TestWarnings(t *testing.T) {
	j := config.NewJob("gece")
	j.Connection.Auth = config.AuthWindows
	j.Backup.ServerPath = `D:\Yedek`
	j.Backup.AccessPath = `\\sql01\Yedek$`
	if w := Warnings(j, ""); len(w) != 1 || !strings.Contains(w[0], `NT AUTHORITY\SYSTEM`) {
		t.Errorf("SYSTEM + Windows: %q", w)
	}
	j.Archive.ZipEnabled = true
	if w := Warnings(j, ""); len(w) != 2 || !strings.Contains(w[1], `\\sql01\Yedek$`) {
		t.Errorf("ağ klasörü: %q", w)
	}
	j.Connection.Auth = config.AuthSQL
	j.Schedule.RunAs = config.RunAsCurrent
	if w := Warnings(j, `PC01\Fatih`); len(w) != 1 || !strings.Contains(w[0], `PC01\Fatih oturum açmışken`) {
		t.Errorf("current: %q", w)
	}
}

func TestTaskDefinitionMatches(t *testing.T) {
	jobs := []*config.Job{
		scheduleJob(nil),
		scheduleJob(func(s *config.Schedule) {
			s.Frequency, s.Days, s.RunAs = config.FrequencyWeekly, []string{"SUN", "MON", "MON"}, config.RunAsCurrent
		}),
		scheduleJob(func(s *config.Schedule) { s.Frequency, s.IntervalHours, s.Time = config.FrequencyHourly, 4, "00:15" }),
	}
	for _, j := range jobs {
		x, err := TaskXML(j, TaskOptions{Command: testCommand(), User: `PC01\Fatih`, Now: testNow})
		if err != nil {
			t.Fatal(err)
		}
		def, err := ParseTaskXML(x)
		if err != nil {
			t.Fatal(err)
		}
		if !def.Matches(j, testCommand()) {
			t.Errorf("%s: eşleşmeli: %+v", j.Schedule.Frequency, def.Schedule)
		}
		changes := []func(*config.Job, *Command){
			func(j *config.Job, _ *Command) { j.Schedule.Time = "04:00" },
			func(j *config.Job, _ *Command) { j.Schedule.RunAs = "x" },
			func(j *config.Job, _ *Command) { j.Schedule.Frequency = "x" },
			func(_ *config.Job, c *Command) { c.Exe = `D:\Yeni\SqlBackup.exe` },
			func(_ *config.Job, c *Command) { c.Home = `D:\Veri` },
		}
		switch j.Schedule.Frequency {
		case config.FrequencyWeekly:
			changes = append(changes, func(j *config.Job, _ *Command) { j.Schedule.Days = []string{"MON"} })
		case config.FrequencyHourly:
			changes = append(changes, func(j *config.Job, _ *Command) { j.Schedule.IntervalHours = 5 })
		}
		for i, ch := range changes {
			j2 := *j
			c := testCommand()
			ch(&j2, &c)
			if def.Matches(&j2, c) {
				t.Errorf("%s: değişiklik %d fark edilmedi", j.Schedule.Frequency, i)
			}
		}
	}
	// Elle değiştirilmiş (tanınmayan) tetikleyici.
	def, _ := ParseTaskXML(`<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><BootTrigger/></Triggers></Task>`)
	if def.Schedule.Frequency != "" || def.Matches(scheduleJob(nil), testCommand()) {
		t.Errorf("%+v", def.Schedule)
	}
}
