package ctxpack

import (
	"fmt"
	"strconv"
	"strings"
)

// MinVersion is the oldest ctxpack this server supports. 0.4.0 introduced the
// exit code 3 contract for JavaScript-rendered pages; earlier releases packed
// those pages into near-empty content and exited 0, which this server has no
// way to detect.
var MinVersion = Version{Major: 0, Minor: 4, Patch: 0}

// Version is a major.minor.patch triple. Pre-release and build suffixes are
// discarded: upstream ships plain releases, and a suffix never changes which
// side of the minimum a version falls on.
type Version struct {
	Major, Minor, Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Less reports whether v precedes other.
func (v Version) Less(other Version) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	return v.Patch < other.Patch
}

// parseVersionOutput extracts the version from `ctxpack --version` output,
// which upstream formats as "ctxpack 0.4.0".
func parseVersionOutput(out string) (Version, error) {
	fields := strings.Fields(out)
	for _, field := range fields {
		if v, err := parseVersion(field); err == nil {
			return v, nil
		}
	}
	return Version{}, fmt.Errorf("no version number in %q", strings.TrimSpace(out))
}

// parseVersion reads a bare "0.4.0" or "v0.4.0", tolerating a "-rc1" or
// "+build" suffix.
func parseVersion(s string) (Version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, fmt.Errorf("not a version: %q", s)
	}

	nums := make([]int, 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("not a version: %q", s)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}
