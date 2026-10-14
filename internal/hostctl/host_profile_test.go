package hostctl

import "testing"

func TestSupportedHostProfiles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts HostFacts
		want  string
	}{
		{"ubuntu22", HostFacts{"linux", "amd64", "ubuntu", "22.04"}, "ubuntu_22.04_linux_amd64"},
		{"ubuntu24", HostFacts{"linux", "amd64", "ubuntu", "24.04"}, "ubuntu_24.04_linux_amd64"},
		{"unqualified_ubuntu", HostFacts{"linux", "amd64", "ubuntu", "26.04"}, ""},
		{"other_distribution", HostFacts{"linux", "amd64", "debian", "24.04"}, ""},
		{"other_architecture", HostFacts{"linux", "arm64", "ubuntu", "24.04"}, ""},
		{"other_os", HostFacts{"darwin", "amd64", "ubuntu", "24.04"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSupportedHost(tc.facts)
			if tc.want == "" {
				if err == nil {
					t.Fatal("unqualified host was accepted")
				}
				return
			}
			if err != nil || tc.facts.profile() != tc.want {
				t.Fatalf("host profile = %q, err = %v; want %q", tc.facts.profile(), err, tc.want)
			}
		})
	}
}

func TestTestHostIdentityIsStillExplicit(t *testing.T) {
	t.Setenv("XINSIGHTCTL_TEST_HOST", "ubuntu-24.04-amd64")
	if _, err := detectSupportedHost(Paths{IsTestRoot: true}); err == nil {
		t.Fatal("test host override must not impersonate the actual Ubuntu 24.04 target")
	}
	t.Setenv("XINSIGHTCTL_TEST_HOST", "ubuntu-22.04-amd64")
	facts, err := detectSupportedHost(Paths{IsTestRoot: true})
	if err != nil || facts.VersionID != "22.04" {
		t.Fatalf("existing explicit test host changed: %+v, %v", facts, err)
	}
}
