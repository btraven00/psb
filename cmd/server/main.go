package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/time/rate"

	"github.com/btraven00/psb/internal/db"
	"github.com/btraven00/psb/internal/handlers"
)

func main() {
	// --- Flags ---
	pflag.Int("port", 8080, "HTTP listen port (used in dev mode)")
	pflag.String("token", "dev-secret", "PSB shared auth token")
	pflag.String("db-type", "sqlite", "Database type: sqlite or postgres")
	pflag.String("database-url", "telemetry.db", "Database connection string")
	pflag.String("tls-domain", "", "Domain for Let's Encrypt autocert (empty = plain HTTP)")
	pflag.String("tls-cache", ".cache", "Directory for autocert certificate cache")
	pflag.Bool("no-rate-limit", false, "Disable rate limiting (for development)")
	pflag.Float64("rate-limit", 10, "Requests per second per IP")
	pflag.Int("rate-burst", 30, "Rate limiter burst size")
	pflag.Parse()

	// --- Viper: bind flags + env ---
	viper.SetEnvPrefix("PSB")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatalf("failed to bind flags: %v", err)
	}

	// Also honour legacy env vars
	viper.BindEnv("port", "APP_PORT")
	viper.BindEnv("token", "PSB_TOKEN")
	viper.BindEnv("db-type", "DB_TYPE")
	viper.BindEnv("database-url", "DATABASE_URL")
	viper.BindEnv("tls-domain", "TLS_DOMAIN")

	// --- Database ---
	database, err := db.InitWith(viper.GetString("db-type"), viper.GetString("database-url"))
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}

	h := &handlers.Handler{
		DB:       database,
		PSBToken: viper.GetString("token"),
	}

	// --- Echo ---
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Rate limiting
	if !viper.GetBool("no-rate-limit") {
		e.Use(middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
			Skipper: middleware.DefaultSkipper,
			Store: middleware.NewRateLimiterMemoryStoreWithConfig(
				middleware.RateLimiterMemoryStoreConfig{
					Rate:  rate.Limit(viper.GetFloat64("rate-limit")),
					Burst: viper.GetInt("rate-burst"),
				},
			),
			IdentifierExtractor: func(ctx echo.Context) (string, error) {
				return ctx.RealIP(), nil
			},
			DenyHandler: func(ctx echo.Context, identifier string, err error) error {
				return ctx.JSON(429, map[string]string{"error": "rate limit exceeded"})
			},
		}))
		log.Printf("rate limiting enabled: %.0f req/s, burst %d",
			viper.GetFloat64("rate-limit"), viper.GetInt("rate-burst"))
	} else {
		log.Println("rate limiting disabled")
	}

	// --- Routes ---
	// Ingestion API
	e.POST("/v1/telemetry", h.PostTelemetry)

	// Public dashboard (at root)
	e.GET("/", h.ViewTelemetry)
	e.GET("/session/:id", h.ViewSession)
	e.GET("/session/:id/jsonl", h.DownloadSessionJSONL)
	e.GET("/session/:id/parquet", h.DownloadSessionParquet)
	e.GET("/env/:id", h.ViewEnv)
	e.GET("/record/:id", h.ViewRecord)
	e.GET("/record/:id/json", h.DownloadRecordJSON)

	// --- Start ---
	tlsDomain := viper.GetString("tls-domain")
	if tlsDomain != "" {
		// Production: Let's Encrypt autocert
		log.Printf("starting with TLS autocert for %s", tlsDomain)
		e.AutoTLSManager.HostPolicy = autocert.HostWhitelist(tlsDomain)
		e.AutoTLSManager.Cache = autocert.DirCache(viper.GetString("tls-cache"))
		e.TLSServer.TLSConfig = &tls.Config{
			GetCertificate: e.AutoTLSManager.GetCertificate,
		}
		e.Logger.Fatal(e.StartAutoTLS(":443"))
	} else {
		// Dev: plain HTTP
		addr := fmt.Sprintf(":%d", viper.GetInt("port"))
		log.Printf("starting HTTP on %s (no TLS)", addr)
		e.Logger.Fatal(e.Start(addr))
	}
}
