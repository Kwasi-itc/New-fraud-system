// Command testcerts creates ephemeral certificates for the disposable test stack.
// None of these keys are suitable for production or committed to the repository.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: testcerts OUTPUT_DIRECTORY")
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Compose may restart this one-shot dependency while MySQL is already
	// running. Never rotate its CA underneath the live server's certificate.
	caBytes, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err == nil {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caBytes) {
			return fmt.Errorf("invalid existing test CA; recreate test volume")
		}
		pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "server.pem"), filepath.Join(dir, "server-key.pem"))
		if err != nil {
			return fmt.Errorf("incomplete test certificates; recreate test volume: %w", err)
		}
		cert, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return err
		}
		_, err = cert.Verify(x509.VerifyOptions{Roots: roots, DNSName: "mysql"})
		return err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, name := range []string{"server.pem", "server-key.pem"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("incomplete test certificate volume; recreate it")
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Disposable migration test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		return err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	server := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "mysql"}, DNSNames: []string{"mysql"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err = x509.CreateCertificate(rand.Reader, server, ca, &serverKey.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "server.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return err
	}
	// Readable by the MySQL container's distinct UID; this volume is exclusively
	// disposable test material and is removed by compose down --volumes.
	return os.WriteFile(filepath.Join(dir, "server-key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0644)
}
