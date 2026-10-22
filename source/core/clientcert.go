package core

// Client certificates (CertFP): the app presents a certificate when it connects over TLS,
// and services can log you in by its fingerprint (SASL EXTERNAL), with no password sent.
// The certificate is kept in config.json; its private key is a secret like a password
// (OS credential store when available, secrets.go).

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"time"
)

type ClientCert struct {
	Cert        string `json:"cert"`        // PEM
	Key         string `json:"key"`         // PEM
	Fingerprint string `json:"fingerprint"` // SHA-256, hex
	SHA512      string `json:"sha512"`
	Subject     string `json:"subject"`
	Expires     string `json:"expires"`
	Error       string `json:"error,omitempty"`
}

// GenerateClientCert makes a new self-signed certificate (ECDSA P-256, 10 years) for nick.
func (a *App) GenerateClientCert(nick string) ClientCert {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return ClientCert{Error: err.Error()}
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	cn := strings.TrimSpace(nick)
	if cn == "" {
		cn = "IRChatterbox"
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"IRChatterbox client certificate"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return ClientCert{Error: err.Error()}
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return ClientCert{Error: err.Error()}
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}))
	return describeCert(certPEM, keyPEM)
}

// ImportClientCert reads a certificate and its private key from a PEM file (both in one
// file, as most clients and guides make them).
func (a *App) ImportClientCert() ClientCert {
	p := a.PickFile("Choose a certificate (.pem with the certificate and private key)")
	if p == "" {
		return ClientCert{}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ClientCert{Error: "Couldn't read that file"}
	}
	if len(b) > 64*1024 {
		return ClientCert{Error: "That file is too big to be a certificate"}
	}
	var certs, keys []string
	rest := b
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		enc := string(pem.EncodeToMemory(blk))
		switch {
		case blk.Type == "CERTIFICATE":
			certs = append(certs, enc)
		case strings.HasSuffix(blk.Type, "PRIVATE KEY"):
			keys = append(keys, enc)
		}
	}
	if len(certs) == 0 || len(keys) == 0 {
		return ClientCert{Error: "The file needs both the certificate and its private key (PEM, unencrypted)"}
	}
	return describeCert(certs[0], keys[0])
}

// DescribeClientCert returns the fingerprints of a saved certificate (for the editor).
func (a *App) DescribeClientCert(certPEM, keyPEM string) ClientCert {
	if certPEM == "" {
		return ClientCert{}
	}
	return describeCert(certPEM, keyPEM)
}

func describeCert(certPEM, keyPEM string) ClientCert {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return ClientCert{Error: "The certificate and private key don't belong together, or the key is encrypted"}
	}
	x, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return ClientCert{Error: "That certificate can't be read"}
	}
	s256 := sha256.Sum256(x.Raw)
	s512 := sha512.Sum512(x.Raw)
	return ClientCert{
		Cert: certPEM, Key: keyPEM,
		Fingerprint: hex.EncodeToString(s256[:]), SHA512: hex.EncodeToString(s512[:]),
		Subject: x.Subject.CommonName, Expires: x.NotAfter.Format("2006-01-02"),
	}
}

// tlsClientCerts returns the certificate to present, if this server has one.
func (s ServerConfig) tlsClientCerts() ([]tls.Certificate, error) {
	if s.ClientCert == "" {
		return nil, nil
	}
	pair, err := tls.X509KeyPair([]byte(s.ClientCert), []byte(s.ClientKey))
	if err != nil {
		return nil, errors.New("the client certificate for this server can't be used (its private key is missing or doesn't match)")
	}
	return []tls.Certificate{pair}, nil
}
