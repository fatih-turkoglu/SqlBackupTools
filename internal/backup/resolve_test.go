package backup

import (
	"reflect"
	"testing"

	"sqlbackup/internal/config"
	"sqlbackup/internal/mssql"
)

func serverDBs() []mssql.Database {
	db := func(name, state string, sys bool) mssql.Database {
		return mssql.Database{Name: name, State: state, IsSystem: sys,
			CanBackup: name != "tempdb" && state == "ONLINE"}
	}
	return []mssql.Database{
		db("master", "ONLINE", true),
		db("tempdb", "ONLINE", true),
		db("model", "ONLINE", true),
		db("msdb", "ONLINE", true),
		db("Satis", "ONLINE", false),
		db("Stok", "OFFLINE", false),
		db("Yeni", "ONLINE", false),
	}
}

func TestResolveSelected(t *testing.T) {
	j := config.NewJob("t")
	j.Databases = []string{"satis", "Stok", "Yok", "master", "Satis", "tempdb"}
	got := ResolveDatabases(j, serverDBs())
	want := []Target{
		{Name: "Satis"},
		{Name: "Stok", Reason: "veritabanı yedeklenebilir durumda değil (OFFLINE)", Failed: true},
		{Name: "Yok", Reason: "sunucuda bulunamadı", Failed: true},
		{Name: "master"},
		{Name: "tempdb", Reason: "tempdb yedeklenemez", Failed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	j.ExcludeSystemDatabases = true
	got = ResolveDatabases(j, serverDBs())
	if got[3] != (Target{Name: "master", Reason: "sistem veritabanları hariç tutuluyor"}) {
		t.Errorf("master atlanmalı: %+v", got[3])
	}
}

func TestResolveIncludeNew(t *testing.T) {
	j := config.NewJob("t")
	j.IncludeNewDatabases = true
	j.ExcludedDatabases = []string{"MODEL", "Satis"}
	got := ResolveDatabases(j, serverDBs())
	want := []Target{
		{Name: "master"},
		{Name: "msdb"},
		{Name: "Stok", Reason: "veritabanı yedeklenebilir durumda değil (OFFLINE)"},
		{Name: "Yeni"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	j.ExcludeSystemDatabases = true
	got = ResolveDatabases(j, serverDBs())
	want = want[2:]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sistem hariç: got %+v\nwant %+v", got, want)
	}
}
