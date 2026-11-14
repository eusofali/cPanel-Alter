// hmea-panel is a lightweight web hosting control panel for Ubuntu/Debian.
package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"hmeapanel/internal/app"
	"hmeapanel/internal/sys"
	"hmeapanel/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hmea-panel:", err)
		os.Exit(1)
	}
}

func run() error {
	dev := flag.Bool("dev", false, "development mode: simulate system changes under <data>/devroot, plain HTTP")
	data := flag.String("data", "", "data directory (default /var/lib/hmea-panel, or ./data in dev mode)")
	listen := flag.String("listen", "", "listen address (default :8443, or 127.0.0.1:8080 in dev mode)")
	cert := flag.String("tls-cert", "", "TLS certificate file (default: self-signed, generated in the data directory)")
	key := flag.String("tls-key", "", "TLS private key file")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: hmea-panel [flags] [serve|set-password|disable-2fa]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *data == "" {
		*data = "/var/lib/hmea-panel"
		if *dev {
			*data = "data"
		}
	}
	if *listen == "" {
		*listen = ":8443"
		if *dev {
			*listen = "127.0.0.1:8080"
		}
	}
	dataDir, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	if !*dev && os.Geteuid() != 0 {
		return errors.New("must run as root to manage system services (use -dev to try it without root)")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	db, err := app.OpenDB(filepath.Join(dataDir, "hmea-panel.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	_ = os.Chmod(filepath.Join(dataDir, "hmea-panel.db"), 0o600)

	a := &app.App{
		DB:  db,
		Log: log,
		Sys: &sys.Sys{Dev: *dev, Root: filepath.Join(dataDir, "devroot"), Log: log},
		UI:  web.FS(),
		TLS: !*dev,
	}

	switch flag.Arg(0) {
	case "set-password":
		fmt.Print("New admin password: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if err := a.SetAdminPassword(strings.TrimRight(line, "\r\n")); err != nil {
			return err
		}
		fmt.Println("Password updated; all sessions were signed out.")
		return nil
	case "disable-2fa":
		if err := a.DisableTwoFactor(); err != nil {
			return err
		}
		fmt.Println("Two-factor sign-in is now off.")
		return nil
	case "", "serve":
	default:
		flag.Usage()
		return fmt.Errorf("unknown command %q", flag.Arg(0))
	}

	if initial, err := a.EnsureAdmin(); err != nil {
		return err
	} else if initial != "" {
		pwFile := filepath.Join(dataDir, "initial-password.txt")
		if err := os.WriteFile(pwFile, []byte(initial+"\n"), 0o600); err != nil {
			return err
		}
		log.Info("first start: admin password generated; change it after signing in", "file", pwFile)
	}

	bg, stopBG := context.WithCancel(context.Background())
	defer stopBG()
	go a.RunScheduler(bg)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}

	errc := make(chan error, 1)
	go func() {
		if *dev {
			log.Info("hmea-panel listening (dev mode, system changes are simulated)", "url", "http://"+*listen)
			errc <- srv.ListenAndServe()
			return
		}
		certFile, keyFile := *cert, *key
		if certFile == "" || keyFile == "" {
			certFile, keyFile = filepath.Join(dataDir, "panel.crt"), filepath.Join(dataDir, "panel.key")
			if err := ensureSelfSigned(certFile, keyFile); err != nil {
				errc <- err
				return
			}
		}
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		log.Info("hmea-panel listening", "addr", *listen)
		errc <- srv.ListenAndServeTLS(certFile, keyFile)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errc:
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// ensureSelfSigned creates a certificate so the panel never serves the admin
// password over plain HTTP. Browsers will warn until a real one is supplied.
func ensureSelfSigned(certFile, keyFile string) error {
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			return nil
		}
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "hmea-panel " + host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	return os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
}
