package accounts

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinCost is the lowest bcrypt cost a hash may have.
	MinCost = 10
	// DefaultCost is the cost Hash uses unless told otherwise.
	DefaultCost = 12
	// MaxPasswordBytes is as much of a password as bcrypt reads. A longer
	// password is refused, never cut to this length.
	MaxPasswordBytes = 72
)

// Errors about a password itself.
var (
	ErrPasswordEmpty   = errors.New("accounts: the password is empty")
	ErrPasswordTooLong = fmt.Errorf("accounts: the password is longer than the %d bytes bcrypt reads", MaxPasswordBytes)
)

// Passwords holds each account's bcrypt hash, by account ID.
type Passwords struct {
	hashes map[string][]byte
}

// Hash returns the account's bcrypt hash.
func (p *Passwords) Hash(accountID string) ([]byte, bool) {
	hash, ok := p.hashes[accountID]
	return hash, ok
}

// ParsePasswords reads an htpasswd file whose names are account IDs. Every
// account in the configuration needs exactly one line, every line has to name
// one of them, and every hash has to be bcrypt at MinCost or more. A refusal
// names lines and account IDs, never a hash.
func ParsePasswords(data []byte, config *Config) (*Passwords, error) {
	if config == nil {
		return nil, errNoAccounts
	}
	known := make(map[string]int)
	for i, account := range config.Accounts {
		known[account.ID] = i
	}
	var problems []string
	add := func(line int, format string, args ...any) {
		problems = append(problems, fmt.Sprintf("line %d: ", line)+fmt.Sprintf(format, args...))
	}
	hashes, lineOf := make(map[string][]byte), make(map[string]int)
	for index, line := range lines(data) {
		number := index + 1
		if len(bytes.TrimSpace(line)) == 0 || line[0] == '#' {
			continue
		}
		name, hash, found := bytes.Cut(line, []byte(":"))
		id := string(name)
		switch {
		case !found:
			add(number, "is not name:hash")
			continue
		case !label.MatchString(id):
			add(number, "names no account; a name is an account ID")
			continue
		}
		if _, ok := known[id]; !ok {
			add(number, "names %s, which is not an account in the configuration", id)
			continue
		}
		if first, seen := lineOf[id]; seen {
			add(number, "repeats the password of %s from line %d", id, first)
			continue
		}
		lineOf[id] = number
		if problem := hashProblem(string(hash)); problem != "" {
			add(number, "the hash for %s %s", id, problem)
			continue
		}
		hashes[id] = hash
	}
	for i, account := range config.Accounts {
		if _, ok := lineOf[account.ID]; !ok && label.MatchString(account.ID) {
			problems = append(problems, fmt.Sprintf("accounts[%d] (%s) has no password", i, account.ID))
		}
	}
	if len(problems) > 0 {
		return nil, &InvalidError{Problems: problems}
	}
	return &Passwords{hashes: hashes}, nil
}

// hashProblem says what is wrong with a hash, or nothing for a bcrypt hash
// this console accepts.
func hashProblem(hash string) string {
	switch {
	case strings.HasPrefix(hash, "$2a$"), strings.HasPrefix(hash, "$2b$"), strings.HasPrefix(hash, "$2y$"):
		cost, err := bcrypt.Cost([]byte(hash))
		switch {
		case err != nil || len(hash) != 60:
			return "is not a whole bcrypt hash"
		case cost < MinCost:
			return fmt.Sprintf("has bcrypt cost %d; the lowest accepted is %d", cost, MinCost)
		}
		return ""
	case strings.HasPrefix(hash, "$apr1$"), strings.HasPrefix(hash, "$1$"):
		return "is MD5; only bcrypt is accepted"
	case strings.HasPrefix(hash, "{SHA}"):
		return "is SHA-1; only bcrypt is accepted"
	case strings.HasPrefix(hash, "$5$"), strings.HasPrefix(hash, "$6$"):
		return "is SHA-crypt; only bcrypt is accepted"
	case strings.HasPrefix(hash, "$2"):
		return "is a bcrypt variant other than $2a$, $2b$ and $2y$"
	}
	return "is crypt or plain text; only bcrypt is accepted"
}

// CheckPassword refuses a password bcrypt would not read whole.
func CheckPassword(password []byte) error {
	switch {
	case len(password) == 0:
		return ErrPasswordEmpty
	case len(password) > MaxPasswordBytes:
		return ErrPasswordTooLong
	}
	return nil
}

// Hash returns the bcrypt hash of a password, for a line of the passwords
// file.
func Hash(password []byte, cost int) ([]byte, error) {
	if err := CheckPassword(password); err != nil {
		return nil, err
	}
	if cost < MinCost || cost > bcrypt.MaxCost {
		return nil, fmt.Errorf("accounts: cost %d is outside %d to %d", cost, MinCost, bcrypt.MaxCost)
	}
	return bcrypt.GenerateFromPassword(password, cost)
}
