package main

import (
	"flag"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"
	"woocart-webdav/version"

	"woocart-webdav/webdav"

	auth "github.com/abbot/go-http-auth"
	"go.uber.org/zap"
)

var davLocation string
var port string
var htpasswd string
var reloadCMD string
var showVersion bool
var showDebug bool
var reloadPHP bool
var log *zap.Logger

const MethodPropfind = "PROPFIND"

func init() {
	log, _ = zap.NewProduction()
	flag.StringVar(&htpasswd, "htpasswd", "/var/www/etc/.credentials.list", "Path to htpasswd file")
	flag.StringVar(&reloadCMD, "reloadcmd", "/usr/bin/pkill -o  -USR2 php-fpm", "Command to call if you want to reload php")
	flag.StringVar(&port, "port", ":8080", "Address where to listen for connections")
	flag.StringVar(&davLocation, "dir", "/var/www/public_html", "Location of root for WebDAV")
	flag.BoolVar(&showVersion, "version", false, "Show build time and version")
	flag.BoolVar(&showDebug, "debug", false, "Show detailed process traces")
}

// BasicAuth wraps a handler requiring HTTP basic auth for it using the given
// username and password and the specified realm, which shouldn't contain quotes.
func BasicAuth(h http.Handler, a *auth.BasicAuth) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if username := a.CheckAuth(r); username == "" {
			a.RequireAuth(w, r)
		} else {
			h.ServeHTTP(w, r)
		}
	})
}

// PHPReloader wraps a handler that signals PHP needs Reloading
func PHPReloader(h http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != MethodPropfind {
			log.Info(r.Method, zap.String("path", r.URL.Path))
		}
		h.ServeHTTP(w, r) // call original
		if strings.HasSuffix(r.URL.Path, ".php") && (r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodPost || r.Method == http.MethodDelete) {
			log.Info("Scheduling Reload", zap.String("path", r.URL.Path))
			reloadPHP = true
		}

	})
}

// MKDIR wraps a handler that creates folder to a file on PUT
// USed for faster curl uploads
func MKDIR(h http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			if strings.Contains(r.UserAgent(), "curl+mkdir") {
				os.MkdirAll(path.Dir(davLocation+r.URL.Path), 0755)
			}
		}
		h.ServeHTTP(w, r) // call original
	})
}

func main() {
	runtime.GOMAXPROCS(1)
	flag.Parse()

	if showDebug {
		log, _ = zap.NewDevelopment()
	}

	log.Info(version.String())
	if showVersion {
		os.Exit(0)
	}

	secret := auth.HtpasswdFileProvider(htpasswd)
	authenticator := auth.NewBasicAuthenticator("WebDAV Server", secret)

	webdavSrv := &webdav.Handler{
		FileSystem: webdav.Dir(davLocation),
		LockSystem: webdav.NewMemLS(),
	}

	http.Handle("/", BasicAuth(MKDIR(PHPReloader(webdavSrv)), authenticator))

	go func() {
		for range time.Tick(time.Second * 5) {
			if reloadPHP {
				cmd := strings.Split(reloadCMD, " ")
				err := exec.Command(cmd[0], cmd[1:]...).Run()
				log.Info("Reloading PHP", zap.Error(err))
				reloadPHP = false
			}
		}
	}()

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("Could not bind to port", zap.Error(err))
	}
}
