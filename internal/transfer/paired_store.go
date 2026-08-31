package transfer

import (
	"encoding/json"
	"os"
	"sync"
)

// PairedDevice is a remembered, trusted peer. The Fingerprint (hex SHA-256 of the
// peer's SPKI) is the stable identity and the map key; Name and IP are cosmetic
// and may change, so they are never used for trust.
type PairedDevice struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	PairedAt    int64  `json:"pairedAt"`
	LastSeen    int64  `json:"lastSeen"`
}

// PairedStore is a JSON-backed set of paired devices keyed by fingerprint.
type PairedStore struct {
	mu   sync.RWMutex
	path string
	byFP map[string]PairedDevice
}

// LoadPairedStore reads the store from path (creating an empty one if absent).
func LoadPairedStore(path string) *PairedStore {
	s := &PairedStore{path: path, byFP: map[string]PairedDevice{}}
	if b, err := os.ReadFile(path); err == nil {
		var list []PairedDevice
		if json.Unmarshal(b, &list) == nil {
			for _, d := range list {
				if d.Fingerprint != "" {
					s.byFP[d.Fingerprint] = d
				}
			}
		}
	}
	return s
}

// IsPaired reports whether a fingerprint is known/trusted.
func (s *PairedStore) IsPaired(fp string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.byFP[fp]
	return ok
}

// Get returns the paired device for a fingerprint.
func (s *PairedStore) Get(fp string) (PairedDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.byFP[fp]
	return d, ok
}

// Add stores (or updates) a paired device and persists.
func (s *PairedStore) Add(d PairedDevice) error {
	s.mu.Lock()
	s.byFP[d.Fingerprint] = d
	s.mu.Unlock()
	return s.save()
}

// Remove forgets a device by fingerprint and persists.
func (s *PairedStore) Remove(fp string) error {
	s.mu.Lock()
	delete(s.byFP, fp)
	s.mu.Unlock()
	return s.save()
}

// List returns all paired devices.
func (s *PairedStore) List() []PairedDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PairedDevice, 0, len(s.byFP))
	for _, d := range s.byFP {
		out = append(out, d)
	}
	return out
}

func (s *PairedStore) save() error {
	s.mu.RLock()
	list := make([]PairedDevice, 0, len(s.byFP))
	for _, d := range s.byFP {
		list = append(list, d)
	}
	s.mu.RUnlock()
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, b, 0o600)
}
