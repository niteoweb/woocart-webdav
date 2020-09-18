package main

import (
	"crypto/subtle"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
	"woocart-webdav/version"

	"go.uber.org/zap"
	"golang.org/x/net/webdav"
)

var davLocation string
var port string
var username string
var password string
var showVersion bool
var showDebug bool
var reloadPHP bool
var log *zap.Logger

const MethodPropfind = "PROPFIND"

func init() {
	log, _ = zap.NewProduction()
	flag.StringVar(&username, "user", "admin", "Username")
	flag.StringVar(&password, "password", "secret", "Password")
	flag.StringVar(&port, "port", ":8080", "Address where to listen for connections")
	flag.StringVar(&davLocation, "dir", "/var/www/public_html", "Location of root for WebDAV")
	flag.BoolVar(&showVersion, "version", false, "Show build time and version")
	flag.BoolVar(&showDebug, "debug", false, "Show detailed process traces")
}

// BasicAuth wraps a handler requiring HTTP basic auth for it using the given
// username and password and the specified realm, which shouldn't contain quotes.
func BasicAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="WebDAV Login"`)
			log.Error("Wrong Password", zap.String("user", user))
			http.Error(w, "Not authorized", 401)
			return
		}

		h.ServeHTTP(w, r) // call original

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

func main() {
	flag.Parse()

	if showDebug {
		log, _ = zap.NewDevelopment()
	}

	log.Info(version.String())
	if showVersion {
		os.Exit(0)
	}

	webdavSrv := &webdav.Handler{
		FileSystem: webdav.Dir(davLocation),
		LockSystem: webdav.NewMemLS(),
	}

	http.Handle("/", BasicAuth(PHPReloader(webdavSrv)))

	go func() {
		for range time.Tick(time.Second * 5) {
			if reloadPHP {
				cmd := exec.Command("pkill", "-o", "-USR2", "php-fpm")
				err := cmd.Run()

				log.Info("Reloading PHP", zap.Error(err))

				reloadPHP = false
			}
		}
	}()

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("Could not bind to port", zap.Error(err))
	}
}
