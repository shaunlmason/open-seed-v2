package artifact

// The store's refusals and its sealed bucket (plans/os-f262585a.md D1,
// D3). The store's whole promise is that a name means its content, so
// the paths worth pinning are the ones where it refuses: a digest that
// is not a digest, content that hashes to something other than the name
// it arrived under, an address the disk cannot serve, and a root the
// filesystem will not let it create.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutVerifiedRefusesContentThatIsNotWhatItWasNamed(t *testing.T) {
	s := Open(t.TempDir())
	body := []byte("the receipt")
	other := Digest([]byte("something else"))

	err := s.PutVerified(other, body)
	if err == nil {
		t.Fatal("content that hashes to another digest must be refused on arrival")
	}
	if !strings.Contains(err.Error(), "refused on arrival") || !strings.Contains(err.Error(), other) {
		t.Errorf("the refusal names what was expected and that it was refused: %v", err)
	}
	if _, err := s.Get(other); err == nil {
		t.Error("the refused content must not be in the store")
	}

	// The honest arrival lands, and reads back byte for byte.
	if err := s.PutVerified(Digest(body), body); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(Digest(body))
	if err != nil || string(got) != string(body) {
		t.Fatalf("the verified put did not round trip: %q %v", got, err)
	}
}

func TestTheStoreRefusesAnythingThatIsNotADigest(t *testing.T) {
	s := Open(t.TempDir())
	for _, bad := range []string{
		"",
		"not-a-digest",
		strings.ToUpper(Digest([]byte("x"))), // uppercase hex is a different name
		Digest([]byte("x"))[:63],
		Digest([]byte("x")) + "0",
		"../../etc/passwd",
	} {
		if err := s.PutVerified(bad, []byte("x")); err == nil {
			t.Errorf("PutVerified(%q) was accepted", bad)
		}
		if _, err := s.Get(bad); err == nil {
			t.Errorf("Get(%q) was accepted", bad)
		}
		if err := s.PutSealed(bad, []byte("x")); err == nil {
			t.Errorf("PutSealed(%q) was accepted", bad)
		}
		if _, err := s.GetSealed(bad); err == nil {
			t.Errorf("GetSealed(%q) was accepted", bad)
		}
	}
}

func TestGetNamesCorruptionRatherThanServingIt(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	digest, err := s.Put([]byte("the receipt"))
	if err != nil {
		t.Fatal(err)
	}
	// Something rewrote the file under its address: the store must not
	// hand that back as if it were addressed content.
	if err := os.WriteFile(filepath.Join(dir, "sha256", digest), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(digest)
	if err == nil {
		t.Fatal("content that no longer hashes to its name must not be returned")
	}
	if got != nil {
		t.Error("a corrupt read returns no bytes at all")
	}
	if !strings.Contains(err.Error(), "the store is corrupt at that address") {
		t.Errorf("the refusal says the store is corrupt: %v", err)
	}
}

func TestGetOnAnAddressTheStoreDoesNotHold(t *testing.T) {
	_, err := Open(t.TempDir()).Get(Digest([]byte("never stored")))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an absent address reports what the filesystem said, got %v", err)
	}
}

func TestSealedCiphertextRotatesInPlace(t *testing.T) {
	// The sealed bucket is deliberately mutable: rotation rewrites the
	// ciphertext under the same commitment, and nothing is
	// digest-checked on the way out, because the commitment verifies
	// the decrypted plaintext rather than these bytes.
	s := Open(t.TempDir())
	commitment := Digest([]byte("the salted commitment"))

	if err := s.PutSealed(commitment, []byte("age-v1 first")); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSealed(commitment, []byte("age-v1 rotated")); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSealed(commitment)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "age-v1 rotated" {
		t.Errorf("rotation replaces the ciphertext in place, got %q", got)
	}

	// The sealed bucket and the content-addressed tree do not collide:
	// the same string names different things in each.
	if _, err := s.Get(commitment); err == nil {
		t.Error("a sealed commitment is not an address in the content-addressed tree")
	}
}

func TestGetSealedOnACommitmentTheStoreDoesNotHold(t *testing.T) {
	_, err := Open(t.TempDir()).GetSealed(Digest([]byte("never sealed")))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an absent commitment reports what the filesystem said, got %v", err)
	}
}

func TestAStoreRootedOnAFileRefusesRatherThanPanics(t *testing.T) {
	// A root that is a file, not a directory: MkdirAll fails and every
	// writing path must surface it.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Open(file)
	if _, err := s.Put([]byte("x")); err == nil {
		t.Error("Put on an unusable root must refuse")
	}
	if err := s.PutSealed(Digest([]byte("c")), []byte("x")); err == nil {
		t.Error("PutSealed on an unusable root must refuse")
	}
}

func TestPutIsIdempotentForTheSameContent(t *testing.T) {
	s := Open(t.TempDir())
	body := []byte("the same bytes")
	first, err := s.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("the same content has one address: %s then %s", first, second)
	}
	got, err := s.Get(first)
	if err != nil || string(got) != string(body) {
		t.Fatalf("the second put left the content intact: %q %v", got, err)
	}
}

func TestAPutTheFilesystemRefusesToLandSurfacesTheError(t *testing.T) {
	// A non-empty directory sitting where the content belongs: the
	// rename cannot replace it. The rival-rename retry reads the
	// address looking for the same bytes landed by someone else, finds
	// a directory instead, and the put fails rather than reporting a
	// success it did not achieve.
	root := t.TempDir()
	body := []byte("the receipt")
	digest := Digest(body)
	occupied := filepath.Join(root, "sha256", digest)
	if err := os.MkdirAll(filepath.Join(occupied, "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(root).Put(body); err == nil {
		t.Fatal("a put that could not land must not report success")
	}

	commitment := Digest([]byte("the commitment"))
	sealed := filepath.Join(root, "sealed", commitment+".age")
	if err := os.MkdirAll(filepath.Join(sealed, "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Open(root).PutSealed(commitment, []byte("age-v1")); err == nil {
		t.Fatal("a sealed put that could not land must not report success")
	}

	// The failed puts leave no temp files behind for a later reader to
	// mistake for content.
	for _, dir := range []string{filepath.Join(root, "sha256"), filepath.Join(root, "sealed")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "put-") || strings.HasPrefix(e.Name(), "seal-") {
				t.Errorf("%s left a temp file behind: %s", dir, e.Name())
			}
		}
	}
}
