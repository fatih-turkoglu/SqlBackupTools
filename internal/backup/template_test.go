package backup

import (
	"testing"
	"time"
)

func TestExpandTemplate(t *testing.T) {
	v := TemplateVars{
		Server:   `PC01\SQLEXPRESS`,
		Database: "Satış:DB",
		Job:      "gece",
		Time:     time.Date(2026, 3, 7, 4, 5, 9, 0, time.Local),
	}
	tests := []struct{ tmpl, want string }{
		{"{db}_FULL_{yyyyMMdd}_{HHmmss}.bak", "Satış_DB_FULL_20260307_040509.bak"},
		{"{server}_{db}_{yyyy-MM-dd}", "PC01_SQLEXPRESS_Satış_DB_2026-03-07.bak"},
		{"{job}.{db}.{yy}{MM}{dd}.{HH}{mm}{ss}.BAK", "gece.Satış_DB.260307.040509.BAK"},
		{"{db}_{yyyyMMdd_HHmmss}.bak", "Satış_DB_20260307_040509.bak"},
		{"{db}_{yyyy_dd}.bak", "Satış_DB_2026_07.bak"},
		{"{db}_{HH.mm}.bak", "Satış_DB_04.05.bak"},
	}
	for _, tt := range tests {
		got, err := ExpandTemplate(tt.tmpl, v)
		if err != nil {
			t.Errorf("%q: %v", tt.tmpl, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.tmpl, got, tt.want)
		}
	}
}

func TestExpandTemplateErrors(t *testing.T) {
	for _, tmpl := range []string{
		"{db}_{tarih}.bak",
		"{db}_{yyyyMMddX}.bak",
		"{db}_{}.bak",
		"{db}_{--}.bak",
		"{db}_{yyyy.bak",
		"{db}}.bak",
		"}{db}.bak",
	} {
		if err := ValidateTemplate(tmpl); err == nil {
			t.Errorf("%q için hata bekleniyordu", tmpl)
		}
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize(" a\\b/c:d*e?f\"g<h>i|j\tk "); got != "a_b_c_d_e_f_g_h_i_j_k" {
		t.Errorf("sanitize: %q", got)
	}
}
