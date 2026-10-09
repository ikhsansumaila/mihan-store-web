package imaging

// Galat penyimpanan berkas gambar (foto produk & bukti transfer) dan pola nama berkas acak.

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
)

const UUIDPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	ErrStoreFull        = errors.New("penyimpanan penuh")
	ErrStoreUnavailable = errors.New("penyimpanan tidak tersedia")
	ErrBadKey           = errors.New("kunci berkas tidak valid")
)

func WrapStoreErr(err error) error {
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return fmt.Errorf("%w: %v", ErrStoreFull, err)
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return err
}
