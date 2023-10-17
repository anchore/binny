package githubrelease

import (
	"fmt"
	"strings"
	"time"
)

type ghRelease struct {
	Tag      string
	Date     *time.Time
	IsLatest *bool
	IsDraft  *bool
	Assets   []ghAsset
}

type ghAsset struct {
	Name        string
	ContentType string
	URL         string
	Checksum    string
}

func (a *ghAsset) addChecksum(value string) {
	if strings.Contains(value, ":") {
		a.Checksum = value
		return
	}

	// note: assume this is a hex digest
	var method string
	switch len(value) {
	case 32:
		method = "md5"
	case 40:
		method = "sha1"
	case 64:
		method = "sha256"
	case 128:
		method = "sha512"
	default:
		// dunno, just capture the value
		a.Checksum = value
		return
	}

	a.Checksum = fmt.Sprintf("%s:%s", method, value)
}
