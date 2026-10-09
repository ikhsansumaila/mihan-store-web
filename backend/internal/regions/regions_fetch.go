package regions

// Pengambil data wilayah dari wilayah.id (JSON statis) yang sopan dan andal:
// - berjenjang: provinces -> regencies per provinsi -> districts per kab/kota -> villages per kecamatan;
// - konkurensi rendah (default 4, maks. 6) + jarak minimal antar permintaan (batas laju global);
// - timeout 15 detik per permintaan, batas ukuran respons (5 MB);
// - retry s.d. 3 kali untuk 429 / 5xx / galat jaringan dengan backoff eksponensial + jitter,
//   menghormati header Retry-After;
// - validasi respons (JSON, `data` array, code & name tidak kosong, format kode per tingkat,
//   kode anak berawalan kode induk, kode unik);
// - gagal total bila provinsi < batas minimal atau permintaan gagal (setelah retry) > MaxFailures.
// Permintaan yang gagal (<= MaxFailures) dicatat sebagai failedKeys: saat Simpan, data lama untuk
// wilayah tersebut (dan turunannya) dipertahankan, tidak dianggap hilang.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"mihanstore/internal/platform"
)

const (
	UserAgent = "MihanStore/1.0 (+https://store.mihan.web.id; admin region sync)"
)

type FetchConfig struct {
	Base          string
	Concurrency   int           // permintaan bersamaan (1..6)
	Interval      time.Duration // jarak minimal antar MULAI permintaan (global)
	Timeout       time.Duration // per permintaan
	MaxRetries    int           // jumlah retry (bukan termasuk percobaan pertama)
	MaxBytes      int64         // ukuran respons maksimal
	MaxFailures   int           // lebih dari ini -> run gagal
	MinProvinces  int           // kurang dari ini -> data tidak masuk akal
	BackoffBase   time.Duration
	BackoffMax    time.Duration
	RetryAfterMax time.Duration
	UserAgent     string
}

func DefaultFetchConfig(base string) FetchConfig {
	if base == "" {
		base = platform.DefaultRegionAPIBase
	}
	return FetchConfig{
		Base: strings.TrimRight(base, "/"), Concurrency: 4, Interval: 100 * time.Millisecond,
		Timeout: 15 * time.Second, MaxRetries: 3, MaxBytes: 5 << 20, MaxFailures: 20, MinProvinces: 30,
		BackoffBase: time.Second, BackoffMax: 30 * time.Second, RetryAfterMax: 60 * time.Second,
		UserAgent: UserAgent,
	}
}

// regionFetchError: galat satu permintaan.
type regionFetchError struct {
	Msg        string
	Status     int
	Retryable  bool
	RetryAfter time.Duration
}

func (e *regionFetchError) Error() string { return e.Msg }

type regionFetcher struct {
	cfg    FetchConfig
	client *http.Client
	sleep  func(ctx context.Context, d time.Duration) error
	jitter func() float64 // 0..1

	paceMu sync.Mutex
	next   time.Time

	requests atomic.Int64
	retries  atomic.Int64
	failed   atomic.Int64
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func newRegionFetcher(cfg FetchConfig) *regionFetcher {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.Concurrency > 6 {
		cfg.Concurrency = 6
	}
	base, _ := url.Parse(cfg.Base)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = cfg.Concurrency
	tr.ResponseHeaderTimeout = cfg.Timeout
	client := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Hanya ikuti redirect ke host yang sama (maks. 3).
			if len(via) >= 3 || base == nil || req.URL.Host != base.Host || req.URL.Scheme != base.Scheme {
				return errors.New("redirect tidak diizinkan")
			}
			return nil
		},
	}
	return &regionFetcher{cfg: cfg, client: client, sleep: sleepCtx, jitter: rand.Float64}
}

// pace menunggu giliran agar jarak antar mulai permintaan >= Interval (batas laju global).
func (f *regionFetcher) pace(ctx context.Context) error {
	f.paceMu.Lock()
	now := time.Now()
	t := f.next
	if t.Before(now) {
		t = now
	}
	f.next = t.Add(f.cfg.Interval)
	f.paceMu.Unlock()
	return f.sleep(ctx, time.Until(t))
}

// backoff: base * 2^attempt dengan jitter (50–100%), dibatasi BackoffMax.
func (f *regionFetcher) backoff(attempt int) time.Duration {
	d := float64(f.cfg.BackoffBase) * math.Pow(2, float64(attempt))
	if d > float64(f.cfg.BackoffMax) {
		d = float64(f.cfg.BackoffMax)
	}
	return time.Duration(d * (0.5 + 0.5*f.jitter()))
}

// parseRetryAfter: detik atau HTTP-date; 0 bila tidak ada/tidak valid.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func regionPath(level int, parent string) string {
	switch level {
	case LevelProvince:
		return "/provinces.json"
	case LevelRegency:
		return "/regencies/" + parent + ".json"
	case LevelDistrict:
		return "/districts/" + parent + ".json"
	default:
		return "/villages/" + parent + ".json"
	}
}

// fetchDataset mengambil dan memvalidasi satu dataset dengan retry.
func (f *regionFetcher) fetchDataset(ctx context.Context, level int, parent string) ([]Item, string, error) {
	u := f.cfg.Base + regionPath(level, parent)
	for attempt := 0; ; attempt++ {
		if err := f.pace(ctx); err != nil {
			return nil, "", err
		}
		items, upd, err := f.once(ctx, u, level, parent)
		if err == nil {
			return items, upd, nil
		}
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		var fe *regionFetchError
		if !errors.As(err, &fe) || !fe.Retryable || attempt >= f.cfg.MaxRetries {
			return nil, "", err
		}
		f.retries.Add(1)
		wait := f.backoff(attempt)
		if fe.RetryAfter > 0 {
			wait = fe.RetryAfter
			if wait > f.cfg.RetryAfterMax {
				wait = f.cfg.RetryAfterMax
			}
		}
		if err := f.sleep(ctx, wait); err != nil {
			return nil, "", err
		}
	}
}

func (f *regionFetcher) once(ctx context.Context, u string, level int, parent string) ([]Item, string, error) {
	f.requests.Add(1)
	rctx, cancel := context.WithTimeout(ctx, f.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", &regionFetchError{Msg: "URL tidak valid"}
	}
	req.Header.Set("User-Agent", f.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", &regionFetchError{Msg: "galat jaringan: " + shortNetErr(err), Retryable: true}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		return nil, "", &regionFetchError{Msg: fmt.Sprintf("HTTP %d", res.StatusCode), Status: res.StatusCode, Retryable: true,
			RetryAfter: parseRetryAfter(res.Header.Get("Retry-After"), time.Now())}
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", &regionFetchError{Msg: fmt.Sprintf("HTTP %d", res.StatusCode), Status: res.StatusCode}
	}
	if res.ContentLength > f.cfg.MaxBytes {
		return nil, "", &regionFetchError{Msg: "respons terlalu besar"}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, f.cfg.MaxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", &regionFetchError{Msg: "gagal membaca respons: " + shortNetErr(err), Retryable: true}
	}
	if int64(len(body)) > f.cfg.MaxBytes {
		return nil, "", &regionFetchError{Msg: "respons terlalu besar"}
	}
	items, upd, err := parseRegionResponse(body, level, parent)
	if err != nil {
		return nil, "", &regionFetchError{Msg: err.Error()}
	}
	return items, upd, nil
}

// shortNetErr: ringkas, tanpa detail panjang.
func shortNetErr(err error) string {
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return platform.TruncateUTF8(s, 80)
}

// parseRegionResponse memvalidasi respons wilayah.id untuk dataset tingkat `level` di bawah `parent`.
func parseRegionResponse(body []byte, level int, parent string) ([]Item, string, error) {
	var env struct {
		Data json.RawMessage `json:"data"`
		Meta struct {
			UpdatedAt string `json:"updated_at"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, "", errors.New("JSON tidak valid")
	}
	d := strings.TrimSpace(string(env.Data))
	if d == "" || d[0] != '[' {
		return nil, "", errors.New("field data bukan array")
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		return nil, "", errors.New("isi data tidak valid")
	}
	items := make([]Item, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for i, m := range raw {
		var code, name string
		if json.Unmarshal(m["code"], &code) != nil || json.Unmarshal(m["name"], &name) != nil {
			return nil, "", fmt.Errorf("item ke-%d: code/name bukan teks", i+1)
		}
		code, name = strings.TrimSpace(code), strings.TrimSpace(name)
		if code == "" || name == "" {
			return nil, "", fmt.Errorf("item ke-%d: code/name kosong", i+1)
		}
		if !validRegionCode(level, code) {
			return nil, "", fmt.Errorf("kode %q tidak sesuai format %s", platform.TruncateUTF8(code, 20), regionLevelNames[level])
		}
		if level > 1 && regionParentOf(code) != parent {
			return nil, "", fmt.Errorf("kode %q tidak berawalan kode induk %s", code, parent)
		}
		if seen[code] {
			return nil, "", fmt.Errorf("kode %q ganda", code)
		}
		if utf8.RuneCountInString(name) > regionNameMaxRunes || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
			!utf8.ValidString(name) {
			return nil, "", fmt.Errorf("nama untuk kode %q tidak valid", code)
		}
		seen[code] = true
		items = append(items, Item{Code: code, Name: name})
	}
	upd := strings.TrimSpace(env.Meta.UpdatedAt)
	if _, err := time.Parse("2006-01-02", upd); err != nil {
		upd = ""
	}
	return items, upd, nil
}

// ---------- Run berjenjang ----------

type regionFetchResult struct {
	Level     int
	Parent    string
	Key       string
	Items     []Item
	UpdatedAt string
}

type regionFetchProgress struct {
	Phase    int `json:"phase"` // tingkat yang sedang diambil (1..4)
	Done     int `json:"done"`
	Total    int `json:"total"`
	Requests int64
	Retries  int64
	Failed   int64
	Items    [5]int64
	Final    bool
}

type regionFetchSummary struct {
	Items           [5]int64 // jumlah item per tingkat
	Datasets        [5]int64 // jumlah dataset per tingkat
	FailedKeys      []string
	SourceUpdatedAt string
	Requests        int64
	Retries         int64
	Failed          int64
	Duration        time.Duration
}

var errRegionTooManyFailures = errors.New("terlalu banyak permintaan gagal")

// Run mengambil semua tingkat. sink dipanggil (berurutan, satu per satu) untuk setiap dataset yang
// valid; progress dipanggil setelah setiap permintaan selesai. Galat dari sink/progress menghentikan run.
func (f *regionFetcher) Run(ctx context.Context, sink func(regionFetchResult) error, progress func(regionFetchProgress) error) (regionFetchSummary, error) {
	start := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var sum regionFetchSummary
	var mu sync.Mutex // melindungi sum, sink, progress
	done, total := 0, 1
	report := func(phase int, final bool) error {
		return progress(regionFetchProgress{Phase: phase, Done: done, Total: total, Requests: f.requests.Load(),
			Retries: f.retries.Load(), Failed: f.failed.Load(), Items: sum.Items, Final: final})
	}
	finish := func() regionFetchSummary {
		sum.Requests, sum.Retries, sum.Failed = f.requests.Load(), f.retries.Load(), f.failed.Load()
		sum.Duration = time.Since(start)
		sort.Strings(sum.FailedKeys)
		return sum
	}
	accept := func(res regionFetchResult) error {
		if err := sink(res); err != nil {
			return err
		}
		sum.Items[res.Level] += int64(len(res.Items))
		sum.Datasets[res.Level]++
		if res.UpdatedAt > sum.SourceUpdatedAt {
			sum.SourceUpdatedAt = res.UpdatedAt
		}
		return nil
	}

	if err := report(1, false); err != nil {
		return finish(), err
	}
	provs, upd, err := f.fetchDataset(ctx, 1, "")
	done++
	if err != nil {
		f.failed.Add(1)
		if ctx.Err() != nil {
			return finish(), ctx.Err()
		}
		return finish(), fmt.Errorf("gagal mengambil daftar provinsi: %w", err)
	}
	if len(provs) < f.cfg.MinProvinces {
		return finish(), fmt.Errorf("data tidak masuk akal: hanya %d provinsi (minimal %d)", len(provs), f.cfg.MinProvinces)
	}
	if err := accept(regionFetchResult{Level: 1, Key: "provinces", Items: provs, UpdatedAt: upd}); err != nil {
		return finish(), err
	}
	parents := make([]string, 0, len(provs))
	for _, p := range provs {
		parents = append(parents, p.Code)
	}
	sort.Strings(parents)
	total += len(parents)
	if err := report(2, false); err != nil {
		return finish(), err
	}

	for level := 2; level <= 4; level++ {
		var next []string
		jobs := make(chan string)
		var wg sync.WaitGroup
		var runErr error
		setErr := func(e error) {
			if runErr == nil {
				runErr = e
				cancel()
			}
		}
		for w := 0; w < f.cfg.Concurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for parent := range jobs {
					items, upd, err := f.fetchDataset(ctx, level, parent)
					mu.Lock()
					done++
					key := regionDatasetKey(level, parent)
					switch {
					case err != nil && ctx.Err() != nil:
						setErr(ctx.Err())
					case err != nil:
						f.failed.Add(1)
						sum.FailedKeys = append(sum.FailedKeys, key)
						if len(sum.FailedKeys) > f.cfg.MaxFailures {
							setErr(fmt.Errorf("%w (%d permintaan gagal setelah retry; terakhir %s: %v)",
								errRegionTooManyFailures, len(sum.FailedKeys), key, err))
						}
					default:
						if e := accept(regionFetchResult{Level: level, Parent: parent, Key: key, Items: items, UpdatedAt: upd}); e != nil {
							setErr(e)
						} else if level < 4 {
							for _, it := range items {
								next = append(next, it.Code)
							}
						}
					}
					if runErr == nil {
						if e := report(level, false); e != nil {
							setErr(e)
						}
					}
					mu.Unlock()
				}
			}()
		}
	feed:
		for _, p := range parents {
			select {
			case jobs <- p:
			case <-ctx.Done():
				break feed
			}
		}
		close(jobs)
		wg.Wait()
		if runErr == nil && ctx.Err() != nil {
			runErr = ctx.Err()
		}
		if runErr != nil {
			return finish(), runErr
		}
		sort.Strings(next)
		parents = next
		if level < 4 {
			total += len(parents)
		}
		if err := report(min(level+1, 4), level == 4); err != nil {
			return finish(), err
		}
	}
	return finish(), nil
}
