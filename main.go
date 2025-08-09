package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ip-api/api"
	"ip-api/config"
	"ip-api/geoip"
	"ip-api/updater"
)

func main() {
	log.Println("Starting IP API server...")

	if _, err := os.Stat(config.App.DataDir); os.IsNotExist(err) {
		if err := os.MkdirAll(config.App.DataDir, 0755); err != nil {
			log.Fatalf("Failed to create data directory: %v", err)
		}
	}

	// 检查数据库文件是否存在，仅在缺失时下载
	cityDBPath := filepath.Join(config.App.DataDir, "GeoLite2-City.mmdb")
	asnDBPath := filepath.Join(config.App.DataDir, "GeoLite2-ASN.mmdb")
	cnDBPath := filepath.Join(config.App.DataDir, "GeoCN.mmdb")

	// 检查所有必需的数据库文件是否存在
	needDownload := false
	if _, err := os.Stat(cityDBPath); os.IsNotExist(err) {
		log.Println("GeoLite2-City.mmdb not found")
		needDownload = true
	}
	if _, err := os.Stat(asnDBPath); os.IsNotExist(err) {
		log.Println("GeoLite2-ASN.mmdb not found")
		needDownload = true
	}
	if _, err := os.Stat(cnDBPath); os.IsNotExist(err) {
		log.Println("GeoCN.mmdb not found")
		needDownload = true
	}

	if needDownload {
		log.Println("Performing initial database download...")
		updater.DownloadAll()
	} else {
		log.Println("All database files exist, skipping initial download")
	}

	if err := geoip.OpenDBs(cityDBPath, asnDBPath, cnDBPath); err != nil {
		log.Fatalf("Could not open GeoIP databases: %v", err)
	}
	defer geoip.CloseDBs()

	ipAPIHandler := http.HandlerFunc(api.IPHandler)
	// 链式中间件：限流 -> CORS -> 实际处理器
	chainedHandler := api.RateLimitMiddleware(api.CorsMiddleware(ipAPIHandler))
	http.Handle("/json/", chainedHandler)
	http.Handle("/json", chainedHandler)

	// 静态地图API路由
	staticMapHandler := http.HandlerFunc(api.StaticMapHandler)
	chainedMapHandler := api.RateLimitMiddleware(api.CorsMiddleware(staticMapHandler))
	http.Handle("/map/", chainedMapHandler)
	http.Handle("/map", chainedMapHandler)

	// 提供主页服务
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "index.html")
		} else {
			http.NotFound(w, r)
		}
	})

	srv := &http.Server{
		Addr:         config.App.ListenAddr,
		ReadTimeout:  config.App.ReadTimeout,
		WriteTimeout: config.App.WriteTimeout,
		IdleTimeout:  config.App.IdleTimeout,
	}

	go func() {
		log.Printf("Server is listening on %s", config.App.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 启动后台更新器
	go updater.Start()

	// 等待中断信号以优雅地关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Create a context with a timeout for the server shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}
