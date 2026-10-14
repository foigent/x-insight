package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestEphemeralReceiverCertificateBindsExactIP(t *testing.T) {
	root := t.TempDir()
	ip := net.ParseIP("198.18.3.7")
	if err := makeTLS(root, "ur-receiver", ip); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(root, "ur-receiver.pem"), filepath.Join(root, "ur-receiver.key"))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(filepath.Join(root, "ur-receiver-ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("CA PEM invalid")
	}
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: ip.String()}); err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("198.18.3.8") == nil {
		t.Fatal("certificate covered the wrong receiver")
	}
	for _, name := range []string{"ur-receiver.pem", "ur-receiver.key", "ur-receiver-ca.pem"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %v", name, info.Mode().Perm())
		}
	}
}
