package brand

import "testing"

// TestPinnedIdentities guards the values external consumers depend on. The
// federation surface and OTEL attribute namespace are deliberately pinned and
// must NOT drift with a binary rename — a change here is a breaking change and
// this test forces it to be conscious.
func TestPinnedIdentities(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Name", Name, "anyctl"},
		{"LegacyName", LegacyName, "labctl"},
		{"EnvPrefix", EnvPrefix, "ANYCTL_"},
		{"LegacyEnvPrefix", LegacyEnvPrefix, "LABCTL_"},
		{"ConfigDirName", ConfigDirName, "anyctl"},
		{"LegacyConfigDirName", LegacyConfigDirName, "labctl"},
		{"Repo", Repo, "jedwards1230/anyctl"},
		{"FederationName", FederationName, "labctl"},
		{"TelemetryPrefix", TelemetryPrefix, "anyctl."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}
