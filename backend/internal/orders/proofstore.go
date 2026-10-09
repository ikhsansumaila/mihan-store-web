package orders

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"mihanstore/internal/platform"
	"mihanstore/internal/platform/imaging"
)

// ---------- Penyimpanan privat ----------

var proofKeyRe = regexp.MustCompile(`^` + imaging.UUIDPattern + `\.jpg$`)

// ValidProofKey: nama berkas bukti sah (UUID v4 acak + .jpg; nama asli dari klien tidak pernah dipakai).
func ValidProofKey(k string) bool { return proofKeyRe.MatchString(k) }

// ProofStore: penyimpanan berkas bukti privat di disk (tidak punya URL publik).
type ProofStore struct{ base string }

func NewProofStore(base string) *ProofStore { return &ProofStore{base: filepath.Clean(base)} }

func (s *ProofStore) path(key string) (string, error) {
	if !proofKeyRe.MatchString(key) {
		return "", imaging.ErrBadKey
	}
	return filepath.Join(s.base, key), nil
}

func newProofKey() (string, error) {
	id, err := platform.NewPublicID() // UUID v4 acak (122 bit)
	if err != nil {
		return "", err
	}
	return id + ".jpg", nil
}

// Save: tulis atomik (temp + fsync + rename), folder 0700, berkas 0600.
func (s *ProofStore) Save(key string, data []byte) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.base, 0o700); err != nil {
		return imaging.WrapStoreErr(err)
	}
	tmp, err := os.CreateTemp(s.base, ".bukti-*.tmp")
	if err != nil {
		return imaging.WrapStoreErr(err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if err := tmp.Sync(); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if err := tmp.Close(); err != nil {
		return imaging.WrapStoreErr(err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return imaging.WrapStoreErr(err)
	}
	ok = true
	return nil
}

// Open membuka berkas untuk dibaca.
func (s *ProofStore) Open(key string) (*os.File, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Delete: berkas yang sudah tidak ada bukan galat (idempoten).
func (s *ProofStore) Delete(key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Writable memeriksa folder bisa ditulis (membuat folder 0700 bila belum ada).
func (s *ProofStore) Writable() error {
	if err := os.MkdirAll(s.base, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.base, ".cek-tulis-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}
