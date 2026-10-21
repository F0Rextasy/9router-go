package browsercookies

import (
	"testing"
)

// TestTmpProbe reads the real browser database on this machine.
func TestTmpProbe(t *testing.T) {
	db, err := FindCookieDB("chrome")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	t.Logf("=> db = %s", db)
	t.Logf("=> lock = %q", DetectProfileLock(db))
	result, err := Read(t.Context(), db)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Logf("=> key_found = %v", result.KeyFound)
	t.Logf("=> decrypted = %d, plain = %d, failed = %d", result.Decrypted, result.Plain, result.Failed)
	t.Logf("=> header length = %d", len(result.Header))
	t.Logf("=> header names = %v", headerNames(result.Header))
	t.Logf("=> expired = %v", result.Expired)
}
