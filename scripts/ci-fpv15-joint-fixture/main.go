// ci-fpv15-joint-fixture creates short-lived TLS and synthetic account inputs
// only on a disposable Linux CI host. Nothing is installed into the product.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/foigent/x-insight/internal/access"
)

type request struct {
	URReceiverIP string `json:"ur_receiver_ip"`
}

func main() {
	if err := create(); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"schema_version": "xinsight.ci-joint-fixture-result.v1", "state": "failed", "category": err.Error()})
		os.Exit(1)
	}
}

func create() error {
	if runtime.GOOS != "linux" || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" || os.Geteuid() == 0 {
		return errors.New("environment_guard")
	}
	var in request
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil {
		return errors.New("input_invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("input_invalid")
	}
	ip := net.ParseIP(in.URReceiverIP)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return errors.New("receiver_ip_invalid")
	}
	runnerTemp := os.Getenv("RUNNER_TEMP")
	if !filepath.IsAbs(runnerTemp) || runnerTemp == "/" {
		return errors.New("runner_temp_invalid")
	}
	root := filepath.Join(runnerTemp, "xinsight-fpv15-joint-private")
	if err := os.Mkdir(root, 0700); err != nil {
		return errors.New("private_root_unavailable")
	}
	adminPassword, err := secret(32)
	if err != nil {
		return errors.New("random_unavailable")
	}
	readerPassword, err := secret(32)
	if err != nil {
		return errors.New("random_unavailable")
	}
	adminSubject, err := subject()
	if err != nil {
		return errors.New("random_unavailable")
	}
	readerSubject, err := subject()
	if err != nil {
		return errors.New("random_unavailable")
	}
	adminHash, err := access.DerivePasswordHash([]byte(adminPassword))
	if err != nil {
		return errors.New("hash_unavailable")
	}
	readerHash, err := access.DerivePasswordHash([]byte(readerPassword))
	if err != nil {
		return errors.New("hash_unavailable")
	}
	accounts := access.AccountConfig{SchemaVersion: access.AccountConfigSchemaV3, Accounts: []access.AccountConfigEntry{
		{SubjectID: adminSubject, Username: "joint.admin", Role: access.RolePlatformAdmin, Disabled: false, PasswordHash: adminHash},
		{SubjectID: readerSubject, Username: "joint.reader", Role: access.RoleWorkbenchReader, Disabled: false, PasswordHash: readerHash},
	}}
	wire, err := json.Marshal(accounts)
	if err != nil {
		return errors.New("accounts_invalid")
	}
	if _, err = access.ParseAccountConfig(wire); err != nil {
		return errors.New("accounts_invalid")
	}
	if err = writePrivate(root, "access-accounts.json", append(wire, '\n')); err != nil {
		return err
	}
	if err = writePrivate(root, "admin.password", []byte(adminPassword+"\n")); err != nil {
		return err
	}
	if err = writePrivate(root, "reader.password", []byte(readerPassword+"\n")); err != nil {
		return err
	}
	if err = makeTLS(root, "ur-receiver", ip); err != nil {
		return err
	}
	if err = makeTLS(root, "xi-proxy", net.ParseIP("127.0.0.1")); err != nil {
		return err
	}
	manifest := map[string]any{"schema_version": "xinsight.ci-joint-fixture-manifest.v1",
		"classification": "ci_ephemeral_only", "private_root": root,
		"account_file":         filepath.Join(root, "access-accounts.json"),
		"admin_password_file":  filepath.Join(root, "admin.password"),
		"reader_password_file": filepath.Join(root, "reader.password"),
		"reader_subject_id":    readerSubject,
		"ur_receiver_ip":       ip.String(),
		"ur_cert_file":         filepath.Join(root, "ur-receiver.pem"),
		"ur_key_file":          filepath.Join(root, "ur-receiver.key"),
		"ur_ca_file":           filepath.Join(root, "ur-receiver-ca.pem"),
		"xi_cert_file":         filepath.Join(root, "xi-proxy.pem"),
		"xi_key_file":          filepath.Join(root, "xi-proxy.key"),
		"xi_ca_file":           filepath.Join(root, "xi-proxy-ca.pem")}
	wire, err = json.Marshal(manifest)
	if err != nil {
		return errors.New("manifest_invalid")
	}
	if err = writePrivate(root, "manifest.json", append(wire, '\n')); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"schema_version": "xinsight.ci-joint-fixture-result.v1", "state": "prepared", "manifest_path": filepath.Join(root, "manifest.json")})
}

func secret(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
func subject() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "xs_" + hex.EncodeToString(raw), nil
}

func writePrivate(root, name string, contents []byte) error {
	path := filepath.Join(root, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("private_write_failed")
	}
	defer f.Close()
	if _, err = f.Write(contents); err != nil {
		return errors.New("private_write_failed")
	}
	if err = f.Sync(); err != nil {
		return errors.New("private_write_failed")
	}
	return nil
}

func makeTLS(root, prefix string, ip net.IP) error {
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return errors.New("tls_random_unavailable")
	}
	caSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return errors.New("tls_random_unavailable")
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{SerialNumber: caSerial, Subject: pkix.Name{CommonName: "CI ephemeral " + prefix + " CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(6 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	if err != nil {
		return errors.New("tls_create_failed")
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return errors.New("tls_create_failed")
	}
	_, leafKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return errors.New("tls_random_unavailable")
	}
	leafSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return errors.New("tls_random_unavailable")
	}
	leafTemplate := &x509.Certificate{SerialNumber: leafSerial, Subject: pkix.Name{CommonName: "CI ephemeral " + prefix},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(4 * time.Hour), IPAddresses: []net.IP{ip},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, ca, leafKey.Public(), caKey)
	if err != nil {
		return errors.New("tls_create_failed")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return errors.New("tls_create_failed")
	}
	if err = writePrivate(root, prefix+"-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})); err != nil {
		return err
	}
	if err = writePrivate(root, prefix+".pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})); err != nil {
		return err
	}
	if err = writePrivate(root, prefix+".key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})); err != nil {
		return err
	}
	return nil
}
