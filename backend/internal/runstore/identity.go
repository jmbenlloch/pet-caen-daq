package runstore

import (
	"runtime"
	"runtime/debug"
)

// CurrentSoftwareIdentity returns reproducible VCS information embedded by the
// Go toolchain. Development builds without VCS metadata explicitly say so.
func CurrentSoftwareIdentity() SoftwareIdentity {
	identity := SoftwareIdentity{Revision: "unknown", GoVersion: runtime.Version()}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return identity
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if setting.Value != "" {
				identity.Revision = setting.Value
			}
		case "vcs.modified":
			identity.Modified = setting.Value == "true"
		}
	}
	return identity
}
