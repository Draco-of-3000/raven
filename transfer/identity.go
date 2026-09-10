package transfer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Identity is this device's long-lived cryptographic identity: a self-signed
// ECDSA P-256 certificate. The device's identity is the SHA-256 of its public
// key (SPKI), NOT the certificate bytes, so re-issuing the cert with the same
// key keeps the same identity. TLS provides encryption; pinning this fingerprint
// provides authentication.
type Identity struct {
	Cert tls.Certificate   // the TLS cert+key, ready for tls.Config.Certificates
	Leaf *x509.Certificate // parsed leaf, for inspection
	FP   string            // fingerprint: hex SHA-256 of the SPKI
}

const (
	keyFileName  = "identity_key.pem"
	certFileName = "identity_cert.pem"
)

// LoadOrCreateIdentity loads the device identity from dir, generating and
// persisting a fresh one on first run (or if the stored files are unparseable).
func LoadOrCreateIdentity(dir string, deviceName string) (*Identity, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("identity dir: %w", err)
	}
	keyPath := filepath.Join(dir, keyFileName)
	certPath := filepath.Join(dir, certFileName)

	if id, err := loadIdentity(certPath, keyPath); err == nil {
		return id, nil
	}
	return createIdentity(dir, certPath, keyPath, deviceName)
}

func loadIdentity(certPath, keyPath string) (*Identity, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	return &Identity{Cert: cert, Leaf: leaf, FP: FingerprintFromCert(leaf)}, nil
}

func createIdentity(dir, certPath, keyPath, deviceName string) (*Identity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("serial: %w", err)
	}
	cn := CleanText(deviceName)
	if cn == "" {
		cn = "Raven device"
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn}, // cosmetic only; never trusted for auth
		NotBefore:             notBeforeFixed(),
		NotAfter:              notBeforeFixed().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create cert: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	// Write the private key owner-only and atomically (temp + rename).
	if err := writeFileAtomic(keyPath, keyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	if err := writeFileAtomic(certPath, certPEM, 0o644); err != nil {
		return nil, fmt.Errorf("write cert: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	return &Identity{Cert: cert, Leaf: leaf, FP: FingerprintFromCert(leaf)}, nil
}

// notBeforeFixed returns a fixed past timestamp so cert validity does not depend
// on wall-clock skew between devices (we ignore chain validity anyway).
func notBeforeFixed() time.Time {
	return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
}

// FingerprintFromCert returns the hex SHA-256 of the cert's SubjectPublicKeyInfo.
func FingerprintFromCert(cert *x509.Certificate) string {
	return fingerprintFromSPKI(cert.RawSubjectPublicKeyInfo)
}

// fingerprintFromRaw parses a raw DER cert (as seen in VerifyPeerCertificate) and
// returns its SPKI fingerprint.
func fingerprintFromRaw(der []byte) (string, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return "", err
	}
	return fingerprintFromSPKI(cert.RawSubjectPublicKeyInfo), nil
}

func fingerprintFromSPKI(spki []byte) string {
	sum := sha256.Sum256(spki)
	return hex.EncodeToString(sum[:])
}

// ShortFingerprint formats the first 8 bytes of a hex fingerprint as colon
// groups for compact display (e.g. "A1:B2:C3:D4:E5:F6:07:18").
func ShortFingerprint(fp string) string {
	fp = strings.ToUpper(fp)
	if len(fp) > 16 {
		fp = fp[:16]
	}
	var groups []string
	for i := 0; i+2 <= len(fp); i += 2 {
		groups = append(groups, fp[i:i+2])
	}
	return strings.Join(groups, ":")
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if the rename succeeded
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
