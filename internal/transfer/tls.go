package transfer

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

// VerifyFunc is called during the TLS handshake with the peer's SPKI fingerprint.
// Returning an error aborts the handshake. The caller injects policy here (is the
// peer paired? are we in pairing mode?). It also records the resolved fingerprint
// so the post-handshake handler knows who it is talking to.
type VerifyFunc func(peerFP string) error

// peerCapture lets a handler learn the verified peer fingerprint after the
// handshake completes (VerifyPeerCertificate runs mid-handshake).
type peerCapture struct {
	fp string
}

// serverTLSConfig builds a mutual-TLS server config that presents our identity,
// requires a client cert, and runs verify against the client's SPKI fingerprint.
// Chain validity is intentionally ignored (certs are self-signed); authentication
// is by pinned fingerprint, performed inside verify.
func serverTLSConfig(id *Identity, verify VerifyFunc, cap *peerCapture) *tls.Config {
	return &tls.Config{
		Certificates:          []tls.Certificate{id.Cert},
		MinVersion:            tls.VersionTLS13,
		ClientAuth:            tls.RequireAnyClientCert,
		InsecureSkipVerify:    true, // we do our own verification in VerifyPeerCertificate
		VerifyPeerCertificate: makeVerify(verify, cap),
	}
}

// clientTLSConfig builds the dialing side's mutual-TLS config.
func clientTLSConfig(id *Identity, verify VerifyFunc, cap *peerCapture) *tls.Config {
	return &tls.Config{
		Certificates:          []tls.Certificate{id.Cert},
		MinVersion:            tls.VersionTLS13,
		InsecureSkipVerify:    true, // self-signed; ServerName is irrelevant
		VerifyPeerCertificate: makeVerify(verify, cap),
	}
}

func makeVerify(verify VerifyFunc, cap *peerCapture) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("peer presented no certificate")
		}
		fp, err := fingerprintFromRaw(rawCerts[0])
		if err != nil {
			return fmt.Errorf("peer certificate unparseable: %w", err)
		}
		if cap != nil {
			cap.fp = fp
		}
		if verify != nil {
			return verify(fp)
		}
		return nil
	}
}
