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
		{"EnvPrefix", EnvPrefix, "ANYCTL_"},
		{"ConfigDirName", ConfigDirName, "anyctl"},
		{"Repo", Repo, "jedwards1230/anyctl"},
		{"FederationName", FederationName, "anyctl"},
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
