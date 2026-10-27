// gateway generates a reviewed vhost; it never edits another application's vhost.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"vps-control/internal/live"
)

var safePath = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)

func render(domain string, https bool, cert, key string, admins []string) (string, error) {
	if !live.Address(domain) || strings.ContainsAny(domain, ":_") {
		return "", errors.New("valid DNS domain or IPv4 required")
	}
	if len(admins) == 0 {
		return "", errors.New("at least one administrator CIDR required")
	}
	allow := ""
	for _, v := range admins {
		p, e := netip.ParsePrefix(v)
		if e != nil || p.Bits() == 0 {
			return "", errors.New("explicit administrator CIDRs required, /0 forbidden")
		}
		allow += "allow " + p.Masked().String() + ";\n"
	}
	listeners := "listen 80;\nlisten [::]:80;\n"
	tlsConfig := ""
	if https {
		if !safePath.MatchString(cert) || !safePath.MatchString(key) {
			return "", errors.New("safe absolute PEM certificate paths required")
		}
		pair, e := tls.LoadX509KeyPair(cert, key)
		if e != nil {
			return "", errors.New("cannot read certificate/key pair")
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil || leaf.VerifyHostname(domain) != nil || time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
			return "", errors.New("certificate hostname or validity check failed")
		}
		listeners = "listen 443 ssl;\nlisten [::]:443 ssl;\n"
		tlsConfig = fmt.Sprintf("ssl_certificate %s;\nssl_certificate_key %s;\nssl_protocols TLSv1.2 TLSv1.3;\n", cert, key)
	}
	config := fmt.Sprintf(`# harbor-managed-v1: generated; validate nginx -t before reload
server {
%s
server_name %s;
%s
client_max_body_size 64k;
add_header X-Content-Type-Options nosniff always;
add_header Referrer-Policy no-referrer always;
location ^~ /s/ {
 access_log off;
 error_log /dev/null;
 proxy_pass http://127.0.0.1:8091;
 proxy_set_header Host $host;
 proxy_set_header Authorization "";
 proxy_hide_header Server;
 proxy_read_timeout 20s;
 proxy_buffering off;
}
location ^~ /api/v1/agent/ {
 access_log off;
 proxy_pass http://127.0.0.1:8080;
 proxy_set_header Host $host;
 proxy_read_timeout 20s;
}
location / {
 %s
 deny all;
 proxy_pass http://127.0.0.1:5173;
 proxy_set_header Host $host;
 proxy_read_timeout 20s;
}
}
`, listeners, domain, tlsConfig, allow)
	if https {
		config += fmt.Sprintf("server { listen 80; listen [::]:80; server_name %s; location ^~ /.well-known/acme-challenge/ { root /var/www/harbor-acme; } location / { return 308 https://%s$request_uri; } }\n", domain, domain)
	}
	return config, nil
}
func main() {
	domain := flag.String("domain", "", "DNS name")
	https := flag.Bool("https", true, "enable TLS; false renders explicit plaintext vhost")
	cert := flag.String("cert", "", "absolute fullchain.pem")
	key := flag.String("key", "", "absolute private key PEM")
	admin := flag.String("admin-cidrs", "", "comma separated trusted administrator CIDRs")
	out := flag.String("out", "", "new output file (never overwritten)")
	shareOnly := flag.Bool("share-only", false, "serve subscriptions only")
	port := flag.Int("listen-port", 0, "explicit HTTP port for IP access")
	flag.Parse()
	config, e := render(*domain, *https, *cert, *key, strings.Split(*admin, ","))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if *port != 0 {
		if *https || *port < 1024 || *port > 65535 || *port == 8080 || *port == 8091 || *port == 5173 {
			fmt.Fprintln(os.Stderr, "invalid HTTP gateway port")
			os.Exit(1)
		}
		config = strings.ReplaceAll(config, "listen 80;", "listen "+strconv.Itoa(*port)+";")
		config = strings.ReplaceAll(config, "listen [::]:80;", "listen [::]:"+strconv.Itoa(*port)+";")
	}
	if *shareOnly {
		a := strings.Index(config, "location ^~ /api/v1/agent/ {")
		b := strings.Index(config[a:], "\n}\n}\n")
		if a < 0 || b < 0 {
			fmt.Fprintln(os.Stderr, "unexpected gateway template")
			os.Exit(1)
		}
		config = config[:a] + "location / { return 404; }\n" + config[a+b+3:]
	}
	if *out == "" {
		fmt.Print(config)
		return
	}
	f, e := os.OpenFile(filepath.Clean(*out), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		fmt.Fprintln(os.Stderr, "output must be a new file")
		os.Exit(1)
	}
	_, e = f.WriteString(config)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "output write failed")
		os.Exit(1)
	}
	fmt.Println("Vhost generated. Run nginx -t, then reload using your existing service manager.")
}
