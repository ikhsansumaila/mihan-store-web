package main

import (
	"context"
	"errors"
	"log"
	"net"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func buildDSN(cfg Config) string {
	c := mysqldrv.NewConfig()
	c.User = cfg.DBUser
	c.Passwd = cfg.DBPassword
	c.Net = "tcp"
	c.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	c.DBName = cfg.DBName
	c.ParseTime = true
	c.Loc = time.UTC
	c.Collation = "utf8mb4_0900_ai_ci"
	c.Timeout = 5 * time.Second
	c.ReadTimeout = 15 * time.Second
	c.WriteTimeout = 15 * time.Second
	c.Params = map[string]string{
		"charset":   "utf8mb4",
		"time_zone": "'+00:00'", // nilai DEFAULT CURRENT_TIMESTAMP juga UTC
	}
	return c.FormatDSN()
}

func openDB(cfg Config) (*gorm.DB, error) {
	gl := logger.New(log.Default(), logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true, // jangan pernah menulis nilai (hash/token) ke log
	})
	db, err := gorm.Open(mysql.Open(buildDSN(cfg)), &gorm.Config{
		Logger:                 gl,
		NowFunc:                func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) },
		SkipDefaultTransaction: true,
		TranslateError:         true,
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// connectWithRetry mencoba terhubung terus (backoff s.d. 30 detik) sampai berhasil.
// Dijalankan di goroutine agar /health dan rute produk tetap melayani selama DB belum siap.
func (a *App) connectWithRetry(cfg Config) {
	delay := time.Second
	for attempt := 1; ; attempt++ {
		db, err := openDB(cfg)
		if err == nil {
			a.db.Store(db)
			log.Printf("database terhubung (percobaan ke-%d)", attempt)
			return
		}
		log.Printf("database belum bisa dihubungi (percobaan ke-%d): %v", attempt, sanitizeDBErr(err))
		time.Sleep(delay)
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

// sanitizeDBErr: pesan error driver tidak memuat password, tapi tetap ringkas.
func sanitizeDBErr(err error) string {
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) {
		return me.Error()
	}
	return err.Error()
}

func isDuplicateKey(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var me *mysqldrv.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
