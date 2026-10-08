// Package notify mengirim notifikasi internal pesanan ke channel Discord (webhook).
//
// Prinsip:
//   - Dikirim SETELAH transaksi commit, secara asinkron (goroutine), timeout 5 detik,
//     satu kali percobaan ulang. Kegagalan tidak pernah menggagalkan pesanan; hanya dicatat
//     di log (tanpa URL webhook).
//   - Isi MINIMAL: nomor pesanan, nama pemesan, jumlah item, total, status, tautan admin.
//     TIDAK ada alamat, telepon, email, atau catatan.
//   - Tidak ada mention: allowed_mentions.parse kosong, markdown dan @ dinetralkan.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Jenis kejadian.
const (
	KindCreated   = "created"
	KindPaid      = "paid"
	KindCancelled = "cancelled"
)

// OrderEvent adalah data yang BOLEH dikirim ke Discord. Sengaja tidak ada field
// alamat/telepon/email/catatan agar tidak mungkin terkirim.
type OrderEvent struct {
	Kind         string
	OrderNo      string
	CustomerName string
	ItemCount    int
	Total        uint64
	Status       string // kode status (pending_confirmation, pending_payment, paid, completed, cancelled)
	AdminURL     string
}

// Notifier adalah antarmuka pengirim notifikasi (bisa diganti tiruan di tes).
type Notifier interface {
	OrderEvent(e OrderEvent)
}

// Noop tidak mengirim apa pun (webhook belum dikonfigurasi).
type Noop struct{}

func (Noop) OrderEvent(OrderEvent) {}

// Discord mengirim ke webhook Discord.
type Discord struct {
	webhookURL string
	client     *http.Client
	timeout    time.Duration
	retryDelay time.Duration
	logf       func(format string, args ...any)
	wg         sync.WaitGroup
}

// Option mengubah perilaku Discord (dipakai tes).
type Option func(*Discord)

func WithTimeout(d time.Duration) Option    { return func(n *Discord) { n.timeout = d } }
func WithRetryDelay(d time.Duration) Option { return func(n *Discord) { n.retryDelay = d } }
func WithLogf(f func(string, ...any)) Option {
	return func(n *Discord) { n.logf = f }
}

// ValidateWebhookURL memeriksa URL webhook. allowAnyHost hanya untuk lingkungan uji
// (server tiruan); di produksi host wajib discord.com / discordapp.com dengan HTTPS.
func ValidateWebhookURL(raw string, allowAnyHost bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return errors.New("URL webhook tidak valid")
	}
	if allowAnyHost {
		if u.Scheme != "http" && u.Scheme != "https" {
			return errors.New("URL webhook harus http(s)")
		}
		return nil
	}
	if u.Scheme != "https" {
		return errors.New("URL webhook harus https")
	}
	switch strings.ToLower(u.Hostname()) {
	case "discord.com", "discordapp.com", "canary.discord.com", "ptb.discord.com":
	default:
		return errors.New("host webhook harus discord.com")
	}
	if !strings.HasPrefix(u.Path, "/api/webhooks/") {
		return errors.New("path webhook harus /api/webhooks/...")
	}
	return nil
}

// New membuat notifier. URL kosong atau tidak valid -> Noop + peringatan di log.
// URL webhook tidak pernah ditulis ke log.
func New(webhookURL string, allowAnyHost bool, opts ...Option) Notifier {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		log.Println("PERINGATAN: notifikasi Discord dinonaktifkan (DISCORD_ORDER_WEBHOOK_URL kosong)")
		return Noop{}
	}
	if err := ValidateWebhookURL(webhookURL, allowAnyHost); err != nil {
		log.Printf("PERINGATAN: notifikasi Discord dinonaktifkan (DISCORD_ORDER_WEBHOOK_URL tidak valid: %v)", err)
		return Noop{}
	}
	d := &Discord{
		webhookURL: webhookURL,
		timeout:    5 * time.Second,
		retryDelay: 2 * time.Second,
		logf:       log.Printf,
	}
	for _, o := range opts {
		o(d)
	}
	d.client = &http.Client{
		Timeout: d.timeout,
		// Jangan ikuti redirect (webhook tidak pernah me-redirect).
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	log.Println("notifikasi Discord aktif")
	return d
}

// OrderEvent mengirim secara asinkron. Tidak pernah memblokir pemanggil.
func (d *Discord) OrderEvent(e OrderEvent) {
	body, err := BuildPayload(e)
	if err != nil {
		d.logf("notifikasi Discord: gagal menyusun pesan: %v", err)
		return
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer func() {
			if v := recover(); v != nil {
				d.logf("notifikasi Discord: panic: %v", v)
			}
		}()
		for attempt := 1; attempt <= 2; attempt++ {
			err := d.send(body)
			if err == nil {
				return
			}
			if attempt == 1 {
				d.logf("notifikasi Discord %s %s gagal (percobaan 1, akan diulang): %v", e.Kind, e.OrderNo, err)
				time.Sleep(d.retryDelay)
				continue
			}
			d.logf("notifikasi Discord %s %s gagal (percobaan 2, menyerah): %v", e.Kind, e.OrderNo, err)
		}
	}()
}

// Wait menunggu semua pengiriman yang sedang berjalan (untuk tes dan shutdown).
func (d *Discord) Wait() { d.wg.Wait() }

func (d *Discord) send(body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("permintaan tidak valid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MihanStore-Notify/1.0")
	res, err := d.client.Do(req)
	if err != nil {
		// *url.Error memuat URL (berisi token webhook): hanya catat penyebab dasarnya.
		var ue *url.Error
		if errors.As(err, &ue) {
			if ue.Timeout() {
				return errors.New("timeout")
			}
			return fmt.Errorf("koneksi gagal: %s", sanitizeErr(ue.Err))
		}
		return errors.New("koneksi gagal")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("status HTTP %d", res.StatusCode)
	}
	return nil
}

// sanitizeErr membuang kemungkinan URL dari pesan error.
func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.Index(s, "http"); i >= 0 {
		s = s[:i] + "[url]"
	}
	return s
}

// ---------- Penyusun pesan ----------

var statusLabels = map[string]string{
	"pending_confirmation": "Menunggu konfirmasi",
	"pending_payment":      "Menunggu pembayaran",
	"paid":                 "Dibayar",
	"completed":            "Selesai",
	"cancelled":            "Dibatalkan",
}

// StatusLabel mengembalikan label Indonesia untuk kode status.
func StatusLabel(s string) string {
	if l, ok := statusLabels[s]; ok {
		return l
	}
	return "-"
}

var titles = map[string]string{
	KindCreated:   "Pesanan baru",
	KindPaid:      "Pesanan dibayar",
	KindCancelled: "Pesanan dibatalkan",
}

var colors = map[string]int{
	KindCreated:   0x7E22CE, // ungu
	KindPaid:      0x16A34A, // hijau
	KindCancelled: 0xDC2626, // merah
}

// FormatRupiah: 1234567 -> "Rp 1.234.567".
func FormatRupiah(n uint64) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s[i : i+3])
	}
	return "Rp " + b.String()
}

// EscapeMarkdown menetralkan markdown Discord dan mention, membuang karakter kontrol,
// dan memotong ke max karakter.
func EscapeMarkdown(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	var clean strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			clean.WriteRune(' ')
		case unicode.IsControl(r), r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F, r == 0x200B:
			// buang
		default:
			clean.WriteRune(r)
		}
	}
	t := strings.Join(strings.Fields(clean.String()), " ")
	if max > 0 && utf8.RuneCountInString(t) > max {
		t = string([]rune(t)[:max]) + "…"
	}
	var b strings.Builder
	for _, r := range t {
		switch r {
		case '\\', '*', '_', '~', '`', '|', '>', '#', '[', ']', '(', ')', '<', ':':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '@':
			// "@" + zero-width space: mention tidak pernah terbentuk.
			b.WriteString("@​")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type embed struct {
	Title       string       `json:"title"`
	URL         string       `json:"url,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color"`
	Fields      []embedField `json:"fields"`
}

type payload struct {
	Username        string          `json:"username"`
	Content         string          `json:"content,omitempty"`
	Embeds          []embed         `json:"embeds"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

// BuildPayload menyusun body JSON webhook. Hanya memakai field OrderEvent (tanpa data pribadi
// selain nama pemesan yang sudah dinetralkan).
func BuildPayload(e OrderEvent) ([]byte, error) {
	title, ok := titles[e.Kind]
	if !ok {
		return nil, fmt.Errorf("jenis notifikasi tidak dikenal: %q", e.Kind)
	}
	orderNo := EscapeMarkdown(e.OrderNo, 24)
	name := EscapeMarkdown(e.CustomerName, 64)
	if name == "" {
		name = "-"
	}
	link := ""
	if u, err := url.Parse(e.AdminURL); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		link = u.String()
	}
	em := embed{
		Title: title + " " + orderNo,
		URL:   link,
		Color: colors[e.Kind],
		Fields: []embedField{
			{Name: "Nomor pesanan", Value: orderNo, Inline: true},
			{Name: "Pemesan", Value: name, Inline: true},
			{Name: "Jumlah item", Value: fmt.Sprintf("%d", e.ItemCount), Inline: true},
			{Name: "Total", Value: FormatRupiah(e.Total), Inline: true},
			{Name: "Status", Value: StatusLabel(e.Status), Inline: true},
		},
	}
	if link != "" {
		em.Description = "Detail: " + link
	}
	p := payload{
		Username:        "Mihan Store",
		Content:         "",
		Embeds:          []embed{em},
		AllowedMentions: allowedMentions{Parse: []string{}},
	}
	return json.Marshal(p)
}
