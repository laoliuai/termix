package relay

import "regexp"

var accessTokenRE = regexp.MustCompile(`(access_token=)[^&]*`)

// ScrubAccessTokenQuery returns a copy of rawQuery with any access_token=...
// parameter value replaced by <redacted>. Use this before writing the request
// URL to logs so short-lived bearer tokens never land on disk.
//
// Example:
//   "access_token=eyJhb..." → "access_token=<redacted>"
//   "x=1&access_token=eyJ&y=2" → "x=1&access_token=<redacted>&y=2"
func ScrubAccessTokenQuery(rawQuery string) string {
	return accessTokenRE.ReplaceAllString(rawQuery, "${1}<redacted>")
}
