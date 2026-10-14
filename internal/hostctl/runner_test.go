package hostctl

import (
	"reflect"
	"testing"
)

func TestPostgresCommandEnvironmentRejectsAmbientLibpqConfiguration(t *testing.T) {
	environment, err := buildCommandEnvironment(Command{
		Name: "pg_dump",
		Env:  map[string]string{"PGPASSWORD": "explicit-password"},
	}, []string{
		"PATH=/usr/bin",
		"PGPASSWORD=ambient-password",
		"PGSERVICE=outside",
		"PGOPTIONS=-c search_path=outside",
		"LANG=C.UTF-8",
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"PATH=/usr/bin", "LANG=C.UTF-8", "PGPASSWORD=explicit-password"}
	if !reflect.DeepEqual(environment, expected) {
		t.Fatalf("environment=%v expected=%v", environment, expected)
	}
}

func TestNonPostgresCommandKeepsUnrelatedEnvironmentAndDeduplicatesOverrides(t *testing.T) {
	environment, err := buildCommandEnvironment(Command{
		Name: "systemctl",
		Env:  map[string]string{"LANG": "C"},
	}, []string{"PATH=/usr/bin", "LANG=C.UTF-8", "PGSERVICE=developer"})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"PATH=/usr/bin", "PGSERVICE=developer", "LANG=C"}
	if !reflect.DeepEqual(environment, expected) {
		t.Fatalf("environment=%v expected=%v", environment, expected)
	}
}
