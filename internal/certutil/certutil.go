// Package certutil generates and persists a self-signed TLS certificate.
package certutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

// LoadOrGenerate loads a TLS certificate from certFile/keyFile.
// If either file is missing it generates a new self-signed P-256 certificate,
// saves it, and returns it.
func LoadOrGenerate(certFile, keyFile string) (tls.Certificate, error) {
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err == nil {
			return cert, nil
		}
		// Fall through to generate if files are missing, fail on parse errors.
		if !os.IsNotExist(err) {
			return tls.Certificate{}, fmt.Errorf("certutil: load cert: %w", err)
		}
	}

	// Auto-generate paths next to the binary if not specified.
	if certFile == "" {
		certFile = "mirage-cert.pem"
	}
	if keyFile == "" {
		keyFile = "mirage-key.pem"
	}

	// Try loading existing generated files first.
	if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
		return cert, nil
	}

	return generate(certFile, keyFile)
}

func generate(certFile, keyFile string) (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certutil: keygen: %w", err)
	}

	serial := new(big.Int)
	serial.SetBytes(func() []byte { b := make([]byte, 16); rand.Read(b); return b }())

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "mirage"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certutil: create cert: %w", err)
	}

	// Save cert.
	cf, err := os.Create(certFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certutil: write %s: %w", certFile, err)
	}
	pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	cf.Close()

	// Save key.
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	kf, err := os.Create(keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certutil: write %s: %w", keyFile, err)
	}
	pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	kf.Close()

	return tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
}
