package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/comms"
	"github.com/go-chi/chi/v5"
	"github.com/jrick/logrotate/rotator"
)

const (
	serverAddress = "127.0.0.1:33893"
)

var (
	siteDir = "site"
	log     dex.Logger

	homeDir, _ = os.UserHomeDir()
	appDir     = filepath.Join(homeDir, ".bworg")
	logDir     = filepath.Join(appDir, "logs")
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		log.Info("Shutting down...")
		cancel()
	}()

	rotator, err := dex.LogRotator(logDir, "bworg.log")
	if err != nil {
		return fmt.Errorf("error initializing log rotator: %w", err)
	}
	log = dex.NewLogger("BW", dex.LevelInfo, &logWriter{rotator})

	comms.UseLogger(log.SubLogger("SRV"))

	srv, err := comms.NewServer(&comms.RPCConfig{
		ListenAddrs: []string{serverAddress},
		NoTLS:       true,
	})
	if err != nil {
		return fmt.Errorf("NewServer error: %w", err)
	}

	mux := srv.Mux()
	// TODO: LimitRate is a little too restrictive for an http site. Figure out
	// a way to customize the rate limits.
	// mux.Use(srv.LimitRate)

	fileServer(mux, "/font")
	fileServer(mux, "/img")
	fileServer(mux, "/js")
	fileServer(mux, "/")

	srv.Run(ctx)

	return nil
}

// fileServer is a file server for files in subDir with the parent folder
// siteDire. An empty siteDir means embedded files only are served. The
// pathPrefix is stripped from the request path when locating the file.
func fileServer(rr chi.Router, pathPrefix string) {
	if strings.ContainsAny(pathPrefix, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	// For the chi.Mux, make sure a path that ends in "/" and append a "*".
	muxRoot := pathPrefix
	if pathPrefix != "/" && pathPrefix[len(pathPrefix)-1] != '/' {
		rr.Get(pathPrefix, http.RedirectHandler(pathPrefix+"/", 301).ServeHTTP)
		muxRoot += "/"
	}
	muxRoot += "*"

	// Mount the http.HandlerFunc on the pathPrefix.
	rr.Get(muxRoot, func(w http.ResponseWriter, r *http.Request) {
		// Ensure the path begins with "/".
		upath := r.URL.Path
		if strings.Contains(upath, "..") {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
			r.URL.Path = upath
		}
		// Strip the path prefix and clean the path.
		upath = path.Clean(upath)
		if upath == "/" {
			upath = "/index.html"
		}

		f, err := os.Open(filepath.Join(siteDir, upath))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		// return in case it is a directory
		stat, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if stat.IsDir() {
			http.NotFound(w, r)
			return
		}

		// http.ServeFile would do the following type detection.
		fileExt := filepath.Ext(upath)
		contentType := mime.TypeByExtension(fileExt)
		if contentType == "" {
			// Sniff out the content type. See http.serveContent.
			var buf [512]byte
			n, _ := io.ReadFull(f, buf[:])
			contentType = http.DetectContentType(buf[:n])
			_, err = f.Seek(0, io.SeekStart) // rewind to output whole file
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} // else don't set it (plain)

		sendSize := stat.Size()
		w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))

		if fileExt != "html" {
			w.Header().Set("Cache-Control", "public, max-age=31536000") // Cache for 1 year
		}

		// TODO: Set Last-Modified for the embedded files.
		// if modTime != nil {
		// 	w.Header().Set("Last-Modified", modTime.Format(http.TimeFormat))
		// }

		_, err = io.CopyN(w, f, sendSize)
		if err != nil {
			log.Errorf("Writing response for path %q failed: %v", r.URL.Path, err)
			// Too late to write to header with error code.
		}
	})
}

func serveFile(w http.ResponseWriter, r *http.Request, fullFilePath string) {
	// Generate the full file system path and test for existence.
	fi, err := os.Stat(fullFilePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Deny directory listings
	if fi.IsDir() {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, fullFilePath)
}

type logWriter struct {
	*rotator.Rotator
}

// Write writes the data in p to the log file.
func (w logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return w.Rotator.Write(p)
}
