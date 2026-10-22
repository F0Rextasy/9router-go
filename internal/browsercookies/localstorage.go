// Package browsercookies reads a browser's cookie database.
//
// The DeepSeek Web credential is two things, and only one of them is a
// cookie: the API authenticates with a `userToken` the site keeps in
// localStorage, and the WAF reads a cookie. A read that returns only the
// cookie would leave the caller without the credential the API actually
// checks.
//
// So this package answers two questions about a browser profile, and each
// has its own answer:
//
//   - the cookies, which live in the browser's SQLite database and are
//     sealed with a key the keyring holds; and
//   - the localStorage values, which live in the profile's LevelDB files and
//     are stored in the clear.
//
// The localStorage read is what makes the DeepSeek sign-in a single step:
// the token is only there, next to the cookies, in the one profile the
// operator is already signed in to.
package browsercookies

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorageValue is one value out of a profile's localStorage.
type LocalStorageValue struct {
	// Origin is the site the value belongs to.
	Origin string

	// Key is the name of the value.
	Key string

	// Value is the stored value.
	Value string
}

// ErrNoLocalStorage means the profile has no localStorage database, or it
// holds nothing for the origin that was asked about.
var ErrNoLocalStorage = errors.New("no localStorage value was found for that origin")

// ReadLocalStorage reads the localStorage values of one origin out of a
// browser profile.
//
// The values live in the profile's "Local Storage/leveldb" directory as
// LevelDB files. They are read by scanning the files for the origin and the
// key rather than by opening the database, because a profile that is
// running holds the database open and a LevelDB read of a live directory is
// not safe: the files change under the reader.
//
// The scan is exact enough for a credential: the origin and the key are
// located as literal bytes, and the value is the JSON-encoded string that
// follows the key.
func ReadLocalStorage(profileDir, origin, key string) (string, error) {
	dir := filepath.Join(profileDir, "Local Storage", "leveldb")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ErrNoLocalStorage
	}

	originBytes := []byte(origin)
	keyBytes := []byte(key)
	// The value is stored as a JSON object whose "value" field holds the
	// string. Both spellings appear in the wild: a bare JSON string, and
	// the object with a version.
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".ldb") && !strings.HasSuffix(name, ".log")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if value, ok := findLocalStorageValue(data, originBytes, keyBytes); ok {
			return value, nil
		}
	}
	return "", ErrNoLocalStorage
}

// findLocalStorageValue locates one localStorage value in a LevelDB file.
// findLocalStorageValue locates one localStorage value in a LevelDB file.
//
// The record is written as the origin, then the key, then the encoded
// value. The value is read out of the JSON that follows, so a profile that
// stores a longer value than another does not truncate the answer.
func findLocalStorageValue(data, origin, key []byte) (string, bool) {
	for offset := 0; offset+len(origin)+len(key) < len(data); {
		found := bytes.Index(data[offset:], origin)
		if found < 0 {
			return "", false
		}
		start := offset + found
		offset = start + len(origin)

		// The key follows the origin.
		gap := offset
		for gap < len(data) && data[gap] != key[0] {
			gap++
		}
		if gap+len(key) >= len(data) {
			return "", false
		}
		if !bytes.Equal(data[gap:gap+len(key)], key) {
			offset = gap + 1
			continue
		}

		value, ok := readJSONString(data[gap+len(key):])
		if ok {
			return value, true
		}
		offset = gap + len(key)
	}
	return "", false
}

// readJSONString reads the string out of a JSON document that follows.
//
// Chromium stores the value as either a bare JSON string or an object with
// a "value" field. Both are read, because which one a profile uses depends
// on the version that wrote it.
func readJSONString(data []byte) (string, bool) {
	limit := 4096
	if len(data) < limit {
		limit = len(data)
	}
	window := data[:limit]

	// The object form: {"value":"…","__version":"0"}.
	if object := bytes.Index(window, []byte(`{"value":"`)); object >= 0 {
		start := object + len(`{"value":"`)
		if end := bytes.Index(window[start:], []byte(`"`)); end >= 0 {
			return string(window[start : start+end]), true
		}
	}
	// The bare form: "…".
	if bare := bytes.Index(window, []byte(`"`)); bare >= 0 {
		start := bare + 1
		if end := bytes.Index(window[start:], []byte(`"`)); end >= 0 {
			return string(window[start : start+end]), true
		}
	}
	return "", false
}

// ProfileFromCookieDB is the profile directory a cookie database belongs to.
//
// Chromium keeps the cookie database and the localStorage database in the
// same profile directory, so a caller that has found one has found the other.
func ProfileFromCookieDB(cookieDBPath string) string {
	return filepath.Dir(cookieDBPath)
}

// bytesIndex is bytes.Index, kept apart so the scanning above reads as one
// operation rather than a mix of helpers.
func bytesIndex(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	limit := len(haystack) - len(needle)
	for i := 0; i <= limit; i++ {
		if bytesEqual(haystack[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

// bytesEqual compares two byte slices.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
