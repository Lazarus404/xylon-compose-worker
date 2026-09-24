package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Lazarus404/xylon-compose-worker/internal/config"
	"github.com/Lazarus404/xylon-compose-worker/internal/httpapi"
	"github.com/Lazarus404/xylon-compose-worker/internal/index"
)

func main() {
	configPath := flag.String("config", "", "path to JSON config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.OutRoot, 0o755); err != nil {
		log.Fatal(err)
	}
	idx, err := index.Open(cfg.IndexPath)
	if err != nil {
		log.Fatal(err)
	}

	srv := &httpapi.Server{Cfg: cfg, Index: idx}
	stop := make(chan struct{})
	srv.StartRetentionLoop(stop)

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		close(stop)
		os.Exit(0)
	}()

	log.Printf("compose listening on http://%s out=%s", cfg.ListenAddr, cfg.OutRoot)
	if err := http.ListenAndServe(cfg.ListenAddr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
