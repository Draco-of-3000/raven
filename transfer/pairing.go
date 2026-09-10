package transfer

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// ConfirmSASFunc shows the 6-digit pairing code to the user and returns true if
// they confirm it matches the code on the other device. This human comparison is
// the out-of-band channel that defeats a man-in-the-middle at first pairing.
type ConfirmSASFunc func(peerName, code string) bool

// sasLabel binds the exported keying material to this app + purpose (RFC 5705).
const sasLabel = "RAVEN-PAIRING-SAS"
const sasContext = "RAVEN-SAS-v1"

// ComputeSAS derives the 6-digit Short Authentication String. Both sides compute
// the identical value because the inputs are canonical: the dialer is always the
// "initiator", the accepter the "responder", and the salt is the TLS session's
// exporter secret (identical on both ends, unknown to anyone outside the
// handshake). A MITM that terminated TLS on each leg presents different certs per
// leg, so the two humans see different codes and decline.
func ComputeSAS(state tls.ConnectionState, initiatorFP, responderFP string) (string, error) {
	salt, err := state.ExportKeyingMaterial(sasLabel, nil, 32)
	if err != nil {
		return "", fmt.Errorf("export keying material: %w", err)
	}
	return sasFromParts(initiatorFP, responderFP, salt), nil
}

// sasFromParts is the pure SAS derivation, separated so it is unit-testable
// without a live TLS session. The salt binds the code to one TLS session.
func sasFromParts(initiatorFP, responderFP string, salt []byte) string {
	h := sha256.New()
	h.Write([]byte(sasContext))
	h.Write([]byte(initiatorFP))
	h.Write([]byte(responderFP))
	h.Write(salt)
	sum := h.Sum(nil)
	n := binary.BigEndian.Uint32(sum[:4]) % 1_000_000
	return fmt.Sprintf("%06d", n)
}

// writeName / readName length-prefix a UTF-8 device name on the wire.
func writeName(conn net.Conn, name string) error {
	b := []byte(CleanText(name))
	if len(b) > maxNameWire {
		b = b[:maxNameWire]
	}
	if err := writeUint32(conn, uint32(len(b))); err != nil {
		return err
	}
	_, err := conn.Write(b)
	return err
}

func readName(conn net.Conn) (string, error) {
	n, err := readUint32(conn)
	if err != nil {
		return "", err
	}
	if n > maxNameWire {
		return "", fmt.Errorf("device name too long")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(conn, b); err != nil {
		return "", err
	}
	return CleanText(string(b)), nil
}

// runPairInitiator runs the dialing side of the PAIR pre-flight. localFP is our
// own fingerprint, peerFP the fingerprint verified during the TLS handshake.
func runPairInitiator(conn *tls.Conn, localName, localFP, peerFP string, confirm ConfirmSASFunc) (PairedDevice, error) {
	// dialer = initiator. Exchange names (cosmetic).
	if err := writeName(conn, localName); err != nil {
		return PairedDevice{}, err
	}
	peerName, err := readName(conn)
	if err != nil {
		return PairedDevice{}, err
	}
	code, err := ComputeSAS(conn.ConnectionState(), localFP, peerFP)
	if err != nil {
		return PairedDevice{}, err
	}
	return finishPairing(conn, peerName, peerFP, code, confirm)
}

// runPairResponder runs the accepting side of the PAIR pre-flight.
func runPairResponder(conn *tls.Conn, localName, localFP, peerFP string, confirm ConfirmSASFunc) (PairedDevice, error) {
	// accepter = responder. Initiator's fingerprint is the peer; ours is responder.
	peerName, err := readName(conn)
	if err != nil {
		return PairedDevice{}, err
	}
	if err := writeName(conn, localName); err != nil {
		return PairedDevice{}, err
	}
	code, err := ComputeSAS(conn.ConnectionState(), peerFP, localFP)
	if err != nil {
		return PairedDevice{}, err
	}
	return finishPairing(conn, peerName, peerFP, code, confirm)
}

// finishPairing shows the SAS, exchanges confirm bytes, and returns the peer to
// store iff BOTH sides confirm. The SAS is computed locally and never sent.
func finishPairing(conn *tls.Conn, peerName, peerFP, code string, confirm ConfirmSASFunc) (PairedDevice, error) {
	mine := byte(0x00)
	if confirm != nil && confirm(peerName, code) {
		mine = 0x01
	}
	if _, err := conn.Write([]byte{mine}); err != nil {
		return PairedDevice{}, err
	}
	theirs := make([]byte, 1)
	if _, err := io.ReadFull(conn, theirs); err != nil {
		return PairedDevice{}, err
	}
	if mine != 0x01 || theirs[0] != 0x01 {
		return PairedDevice{}, fmt.Errorf("pairing not confirmed on both devices")
	}
	return PairedDevice{Name: peerName, Fingerprint: peerFP}, nil
}
