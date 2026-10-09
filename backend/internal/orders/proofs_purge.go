package orders

import (
	"context"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
)

// ---------- Retensi (180 hari) ----------

// PurgePaymentProofs menghapus bukti pesanan yang selesai/dibatalkan lebih dari 180 hari lalu: berkas dulu
// (berkas yang sudah hilang tidak dianggap galat), lalu baris DB + satu log aktivitas ringkasan dalam satu
// transaksi. Berkas yang gagal dihapus (selain "tidak ada") dilewati dan dicoba lagi pada putaran berikut.
func (a *Service) PurgePaymentProofs(ctx context.Context, now time.Time) (int, error) {
	db := a.db()
	if db == nil || a.Proofs == nil {
		return 0, nil
	}
	db = db.WithContext(ctx)
	cutoff := now.Add(-proofRetentionDays * 24 * time.Hour)
	var rows []struct {
		ID      uint64 `gorm:"column:id"`
		FileKey string `gorm:"column:file_key"`
		OrderNo string `gorm:"column:order_no"`
	}
	if err := db.Raw(`SELECT p.id, p.file_key, o.order_no FROM order_payment_proofs p JOIN orders o ON o.id = p.order_id
		WHERE (o.status = 'completed' AND o.completed_at IS NOT NULL AND o.completed_at < ?)
		   OR (o.status = 'cancelled' AND o.cancelled_at IS NOT NULL AND o.cancelled_at < ?)
		ORDER BY p.id LIMIT 1000`, cutoff, cutoff).Scan(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	ids := make([]uint64, 0, len(rows))
	nos := make([]string, 0, len(rows))
	for _, r := range rows {
		if err := a.Proofs.Delete(r.FileKey); err != nil {
			log.Printf("purge bukti pembayaran: berkas %s gagal dihapus, dicoba lagi nanti: %v", r.OrderNo, err)
			continue
		}
		ids = append(ids, r.ID)
		if len(nos) < 50 {
			nos = append(nos, r.OrderNo)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var deleted int64
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`DELETE FROM order_payment_proofs WHERE id IN ?`, ids)
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return a.audit.Log(tx, audit.Entry{
			ActorLabel: "sistem", Action: "order.payment_proof_purge", EntityType: "order",
			Summary: fmt.Sprintf("%d bukti transfer dihapus otomatis (retensi %d hari)", deleted, proofRetentionDays),
			Details: map[string]any{"jumlah": deleted, "retensiHari": proofRetentionDays, "pesanan": nos},
		})
	})
	return int(deleted), err
}

// StartProofPurger menjalankan purge saat start (setelah jeda) lalu tiap 24 jam, berhenti saat bgCtx selesai.
func (a *Service) StartProofPurger(ctx context.Context) {
	go func() {
		t := time.NewTimer(2 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			n, err := a.PurgePaymentProofs(ctx, a.now())
			if err != nil {
				log.Printf("purge bukti pembayaran: %v", err)
			} else if n > 0 {
				log.Printf("purge bukti pembayaran: %d bukti dihapus", n)
			}
			t.Reset(proofPurgeInterval)
		}
	}()
}
