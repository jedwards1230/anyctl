package compat

import "testing"

func TestGetenvPrefersNewName(t *testing.T) {
	t.Setenv("ANYCTL_THING", "new")
	t.Setenv("LABCTL_THING", "old")
	if got := Getenv("ANYCTL_THING", "LABCTL_THING"); got != "new" {
		t.Fatalf("Getenv = %q, want new (the preferred name wins)", got)
	}
}

func TestGetenvFallsBackToLegacy(t *testing.T) {
	t.Setenv("ANYCTL_ONLYLEGACY", "")
	t.Setenv("LABCTL_ONLYLEGACY", "legacy")
	if got := Getenv("ANYCTL_ONLYLEGACY", "LABCTL_ONLYLEGACY"); got != "legacy" {
		t.Fatalf("Getenv = %q, want legacy fallback", got)
	}
}

func TestGetenvUnsetReturnsEmpty(t *testing.T) {
	if got := Getenv("ANYCTL_UNSET_XYZ", "LABCTL_UNSET_XYZ"); got != "" {
		t.Fatalf("Getenv = %q, want empty when neither is set", got)
	}
}

func TestLegacyEnvSet(t *testing.T) {
	t.Setenv("ANYCTL_LE", "")
	t.Setenv("LABCTL_LE", "x")
	if !LegacyEnvSet("ANYCTL_LE", "LABCTL_LE") {
		t.Fatal("LegacyEnvSet = false, want true when only legacy is set")
	}
	t.Setenv("ANYCTL_LE", "y")
	if LegacyEnvSet("ANYCTL_LE", "LABCTL_LE") {
		t.Fatal("LegacyEnvSet = true, want false when the new name is set")
	}
}
