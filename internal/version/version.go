package version

import (
	_ "embed"
	"strings"
)

//go:embed version.txt
var embeddedVersion string

var (
	Version   string
	Commit    string
	Revision  string
	BuildDate string
	BuiltBy   string
)

func GetVersion() string {
	if strings.TrimSpace(Version) != "" {
		return strings.TrimSpace(Version)
	}

	return strings.TrimSpace(embeddedVersion)
}
