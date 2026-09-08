package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPutGetRoundTrip(t *testing.T) {
	s := Open(t.TempDir())
	body := []byte(`{"receipt": true}`)
	digest, err := s.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		t.Fatalf("digest %q is not lowercase-hex sha256", digest)
	}
	got, err := s.Get(digest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("round trip lost bytes: %q", got)
	}
	if Digest(body) != digest {
		t.Fatalf("Digest disagrees with Put")
	}
}

func TestGetRefusesCorruptContent(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	digest, err := s.Put([]byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sha256", digest), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(digest); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt-on-disk content must refuse naming corruption, got %v", err)
	}
}

func TestGetRefusesBadDigestForm(t *testing.T) {
	s := Open(t.TempDir())
	for _, bad := range []string{"", "abc", strings.Repeat("A", 64), "../escape"} {
		if _, err := s.Get(bad); err == nil {
			t.Fatalf("digest %q must refuse", bad)
		}
	}
}

func TestConcurrentPutsOfSameContent(t *testing.T) {
	s := Open(t.TempDir())
	body := []byte(strings.Repeat("x", 4096))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Put(body); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if _, err := s.Get(Digest(body)); err != nil {
		t.Fatal(err)
	}
}

// conformance: III.A row 7 — the erasure path is a store operation
// (plans/os-db5cd353.md D5): it empties whichever buckets hold the
// digest, names them, and reports nothing for a digest nothing holds,
// since the record is the attribution and the bytes' absence is not
// an error.
func TestEraseEmptiesEachBucketAndNamesThem(t *testing.T) {
	s := Open(t.TempDir())
	digest, err := s.Put([]byte("a receipt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutSealed(digest, []byte("age-encryption.org/v1\n")); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Erase(digest)
	if err != nil || strings.Join(removed, ",") != "sealed,content" {
		t.Fatalf("both buckets emptied and named: %v %v", removed, err)
	}
	if _, err := s.Get(digest); err == nil {
		t.Fatal("the content is gone")
	}
	if _, err := s.GetSealed(digest); err == nil {
		t.Fatal("the ciphertext is gone")
	}
	if removed, err := s.Erase(digest); err != nil || len(removed) != 0 {
		t.Fatalf("erasing what is already gone removes nothing and is not an error: %v %v", removed, err)
	}
	if _, err := s.Erase("not a digest"); err == nil {
		t.Fatal("the digest form is held")
	}
}
