package main

import "testing"

// The version the executable reports (spec #150, #127): the linker-injected
// version when there is one, else the build info's module version without the
// leading v, but only when it is a release tag. Anything else from build
// metadata never reaches initialize or the startup log: it reports 0.0.0-dev.
func TestReportedVersionMapsTheBuildVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, injected, module, want string
	}{
		{"release tag", "", "v0.1.0", "0.1.0"},
		{"release tag with multi-digit parts", "", "v12.30.405", "12.30.405"},
		{"pre-release tag", "", "v0.2.0-rc.1", "0.2.0-rc.1"},
		{"pseudo-version without a tag", "", "v0.0.0-20260928000103-0f5f06470bc1", developmentVersion},
		{"pseudo-version after a release tag", "", "v0.1.1-0.20260928000103-0f5f06470bc1", developmentVersion},
		{"pseudo-version after a pre-release tag", "", "v0.2.0-rc.1.0.20260928000103-0f5f06470bc1", developmentVersion},
		{"devel", "", "(devel)", developmentVersion},
		{"empty", "", "", developmentVersion},
		{"release tag of a dirty checkout", "", "v0.1.0+dirty", developmentVersion},
		{"pseudo-version of a dirty checkout", "", "v0.0.0-20260928000103-0f5f06470bc1+dirty", developmentVersion},
		{"no leading v", "", "0.1.0", developmentVersion},
		{"leading zero", "", "v0.01.0", developmentVersion},
		{"two parts", "", "v0.1", developmentVersion},
		{"injected text after a tag", "", "v0.1.0\n{\"msg\":\"forged\"}", developmentVersion},
		{"markup in a pre-release", "", "v0.1.0-<b>x</b>", developmentVersion},
		{"control character in a pre-release", "", "v0.1.0-rc\x1b[31m", developmentVersion},
		{"linker-injected value wins over a release tag", "1.4.2-SNAPSHOT-0a1b2c3", "v0.1.0", "1.4.2-SNAPSHOT-0a1b2c3"},
		{"linker-injected value wins over a pseudo-version", "0.3.0", "v0.0.0-20260928000103-0f5f06470bc1", "0.3.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reportedVersion(tc.injected, tc.module); got != tc.want {
				t.Errorf("reportedVersion(%q, %q) = %q, want %q", tc.injected, tc.module, got, tc.want)
			}
		})
	}
}
