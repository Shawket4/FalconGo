package main

import (
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
)

// startProfiler serves net/http/pprof when FALCON_PPROF_ADDR is set, so a CPU
// profile can be taken from production and committed as default.pgo for
// profile-guided optimization:
//
//	curl -o default.pgo "http://127.0.0.1:6060/debug/pprof/profile?seconds=120"
//
// Off unless the variable is set, and it only binds a loopback address: the
// profiler exposes stack traces and must never be reachable from outside.
func startProfiler() {
	addr := os.Getenv("FALCON_PPROF_ADDR")
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		log.Printf("FALCON_PPROF_ADDR=%q ignored: use a loopback address like 127.0.0.1:6060", addr)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("pprof listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("pprof stopped: %v", err)
		}
	}()
}
