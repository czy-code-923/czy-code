package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Trust a disposable CA only in a child test process. This exercises the public
// certificate validation and database propagation without altering system trust.
func TestManagedCertificateNoopPreservesNodeVersions(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("Linux isolated trust store and PostgreSQL required")
	}
	if os.Getenv("STARHUB_CERT_NOOP_CHILD") != "1" {
		dir := t.TempDir()
		rootKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable regression CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
		rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"cert-noop.example.com"}, NotBefore: root.NotBefore, NotAfter: root.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{"root.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), "leaf.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "key.pem": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
				t.Fatal(err)
			}
		}
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestManagedCertificateNoopPreservesNodeVersions$", "-test.v")
		cmd.Env = append(os.Environ(), "STARHUB_CERT_NOOP_CHILD=1", "STARHUB_CERT_NOOP_DIR="+dir, "SSL_CERT_FILE="+filepath.Join(dir, "root.pem"))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated certificate regression: %v\n%s", err, output)
		}
		return
	}
	f := newSubscriptionFixture(t, 0)
	dir := os.Getenv("STARHUB_CERT_NOOP_DIR")
	cert, _ := os.ReadFile(filepath.Join(dir, "leaf.pem"))
	key, _ := os.ReadFile(filepath.Join(dir, "key.pem"))
	body := map[string]any{"domain": "cert-noop.example.com", "provider": "manual", "certificate": string(cert), "private_key": string(key)}
	managed := obj018(f.h.call("POST", "certificates", body, 201, nil))
	node := obj018(f.h.call("POST", "live", map[string]any{"name": "TLS regression", "host_id": f.host["id"], "engine": "sing-box", "kind": "trojan", "address": "cert-noop.example.com", "listen": "0.0.0.0", "port": 24567, "certificate_id": managed["id"], "server_name": "cert-noop.example.com"}, 201, nil))
	before := obj018(f.h.call("GET", "live/"+node["id"].(string), nil, 200, nil))
	f.h.call("PUT", "certificates/"+managed["id"].(string), body, 200, nil)
	after := obj018(f.h.call("GET", "live/"+node["id"].(string), nil, 200, nil))
	if before["version"] == nil || before["version"] != after["version"] {
		t.Fatal("same certificate restarted a bound node", before["version"], after["version"])
	}
}
