package config

import (
	"testing"
)

func TestDefaultsAndRoundTrip(t *testing.T) {
	d := Defaults()
	if d.MaxIterations != 500 || d.Timezone == "" {
		t.Fatalf("defaults = %+v", d)
	}
	dir := t.TempDir()
	d.Model = "x-model"
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	back := Load(dir)
	if back.Model != "x-model" {
		t.Fatalf("round trip = %+v", back)
	}
	if got := Load(t.TempDir()); got.Model == "" {
		t.Fatal("missing file must yield defaults")
	}
}

func TestNowRespectsEnv(t *testing.T) {
	t.Setenv("BOTREAPER_TIMEZONE", "UTC")
	if Now().Location().String() != "UTC" {
		t.Fatalf("tz = %s", Now().Location())
	}
	ResetTimezoneCache()
}
