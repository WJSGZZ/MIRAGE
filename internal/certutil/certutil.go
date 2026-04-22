// Package certutil generates and persists a self-signed TLS certificate.
package certutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
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

// LeafCert returns the parsed leaf certificate of a tls.Certificate.
func LeafCert(cert tls.Certificate) (*x509.Certificate, error) {
	if cert.Leaf != nil {
		return cert.Leaf, nil
	}
	if len(cert.Certificate) == 0 {
		return nil, fmt.Errorf("certutil: certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("certutil: parse leaf certificate: %w", err)
	}
	return leaf, nil
}

// SPKIPinBase64URL returns the spec pin format:
// SHA-256(SubjectPublicKeyInfo DER), base64url without padding.
func SPKIPinBase64URL(cert tls.Certificate) (string, error) {
	leaf, err := LeafCert(cert)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
