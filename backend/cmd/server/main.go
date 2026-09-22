package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"goviz/backend/internal/api"
	"goviz/backend/internal/compiler"
	"goviz/backend/internal/engine"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address (loopback only)")
	static := flag.String("static", "../frontend/dist", "built frontend directory; empty disables static serving")
	timeout := flag.Duration("build-timeout", 30*time.Second, "maximum duration of a compiler invocation")
	flag.IntVar(&engine.MaxNodes, "max-nodes", 500, "maximum number of IR nodes")
	flag.IntVar(&engine.MaxCapacity, "max-capacity", 1024, "maximum channel capacity")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || (host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback())) {
		log.Fatal("-addr must be a loopback host:port")
	}
	if engine.MaxNodes < 1 || engine.MaxCapacity < 0 || *timeout <= 0 {
		log.Fatal("invalid resource limits")
	}
	service := &api.Server{Compiler: compiler.New(*timeout)}
	mux := http.NewServeMux()
	mux.Handle("/api/", service.Handler())
	if *static != "" {
		if info, err := os.Stat(filepath.Join(*static, "index.html")); err == nil && !info.IsDir() {
			mux.Handle("/", http.FileServer(http.Dir(*static)))
		} else {
			log.Print("Frontend build not found; use Vite dev server or run npm run build in frontend")
		}
	}
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: *timeout + 15*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	fmt.Printf("GoViz v0 listening on http://%s\n", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
