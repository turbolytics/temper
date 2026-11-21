// Command counter is a fixture for temper scenarios run.
//
//	counter serve                       POST /inc adds one to a counter on disk
//	counter serve -racy                 the same, without the lock: concurrent increments clobber
//	counter load -n N -url U -out FILE  sends N increments at once, records the acks
//
// serve writes the new count to /data/count and fsyncs it before it replies,
// so an acknowledged increment is durable. load writes how many increments
// were acknowledged to FILE once every request has finished.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: counter serve | counter load -n N -url U -out FILE")
	}
	switch os.Args[1] {
	case "serve":
		serve("/data/count", len(os.Args) > 2 && os.Args[2] == "-racy")
	case "load":
		load(os.Args[2:])
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func read(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return n
}

func serve(path string, racy bool) {
	var mu sync.Mutex
	n := read(path)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	http.HandleFunc("/inc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		if racy {
			// Read, add one, write back, with no lock: two requests that read
			// the same count both write count+1, and one increment is lost.
			v := read(path) + 1
			if err := write(path, v); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			fmt.Fprintln(w, v)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if err := write(path, n+1); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		n++
		fmt.Fprintln(w, n)
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// write replaces path with v: write a temporary file, fsync it, rename it.
func write(path string, v int) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".count-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := fmt.Fprintln(tmp, v); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func load(args []string) {
	fs := flag.NewFlagSet("load", flag.ExitOnError)
	n := fs.Int("n", 1000, "increments to send at once")
	url := fs.String("url", "http://http:8080/inc", "increment endpoint")
	out := fs.String("out", "/data/acked", "where to record the acknowledged count")
	_ = fs.Parse(args)

	client := &http.Client{Timeout: 60 * time.Second}
	var acked atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < *n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, err := client.Post(*url, "text/plain", nil)
			if err != nil {
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				acked.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if err := write(*out, int(acked.Load())); err != nil {
		log.Fatal(err)
	}
	fmt.Println(acked.Load())
}
