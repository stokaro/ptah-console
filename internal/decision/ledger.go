// Package decision keeps the console's record of the decisions it sent.
//
// A decision's object name is derived from who decided, what about, and the
// exact thing decided, so sending the same decision again names the same
// object: a retry after a lost response finds the first one instead of
// creating a second. Nothing here is replayed or queued; after a restart the
// ledger is empty and the answer comes from Kubernetes, by name.
package decision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stokaro/ptah-console/internal/kube"
)

// Errors a caller tells apart.
var (
	ErrNotFound = errors.New("decision: no such request")
	ErrNotYours = errors.New("decision: the request belongs to another person")
)

// Key is what a decision is about.
type Key struct {
	// Action names the decision, such as "approve-schema-plan".
	Action    string
	Namespace string
	// TargetUID is the object the decision concerns.
	TargetUID string
	// Payload is the exact thing decided: a plan UID and fingerprint, or an
	// operation ID.
	Payload string
}

// Entry is one decision sent.
type Entry struct {
	RequestID string
	// Name is the Kubernetes object name the decision is created under.
	Name      string
	Principal kube.Principal
	Key       Key
	Sent      time.Time
}

// Ledger holds the decisions sent by this process.
type Ledger struct {
	mu      sync.Mutex
	entries map[string]Entry
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{entries: make(map[string]Entry)}
}

// Begin returns the entry for a decision, the same one every time the same
// person sends the same decision. prefix opens the object name.
func (l *Ledger) Begin(principal kube.Principal, key Key, prefix string) Entry {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(principal.Mode), principal.AccountID, principal.Context, strconv.FormatUint(principal.Generation, 10),
		key.Action, key.Namespace, key.TargetUID, key.Payload,
	}, "\x00")))
	id := hex.EncodeToString(sum[:])
	entry := Entry{RequestID: id[:32], Name: prefix + id[:20], Principal: principal, Key: key, Sent: time.Now()}

	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.entries[entry.RequestID]; ok {
		return existing
	}
	l.entries[entry.RequestID] = entry
	return entry
}

// Get returns the entry for a request ID, and refuses one another person
// sent.
func (l *Ledger) Get(principal kube.Principal, requestID string) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[requestID]
	if !ok {
		return Entry{}, ErrNotFound
	}
	if entry.Principal != principal {
		return Entry{}, ErrNotYours
	}
	return entry, nil
}
