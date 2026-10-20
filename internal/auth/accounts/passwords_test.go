package accounts_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	qt "github.com/frankban/quicktest"
	"golang.org/x/crypto/bcrypt"

	"github.com/stokaro/ptah-console/internal/auth/accounts"
)

// hashes are bcrypt hashes at the lowest accepted cost, made once.
var hashes = sync.OnceValues(func() (map[string]string, error) {
	made := make(map[string]string)
	for _, password := range []string{"alex-secret", "sam-secret"} {
		hash, err := accounts.Hash([]byte(password), accounts.MinCost)
		if err != nil {
			return nil, err
		}
		made[password] = string(hash)
	}
	return made, nil
})

func hashOf(t *testing.T, password string) string {
	t.Helper()
	made, err := hashes()
	qt.New(t).Assert(err, qt.IsNil)
	return made[password]
}

func exampleConfig(t *testing.T) *accounts.Config {
	t.Helper()
	config, err := accounts.ParseConfig(example(t))
	qt.New(t).Assert(err, qt.IsNil)
	return config
}

func TestEachAccountSignsInWithItsOwnHash(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	file := "# written by hand\r\n" +
		"alex:" + hashOf(t, "alex-secret") + "\r\n" +
		"\n" +
		"sam:" + hashOf(t, "sam-secret") + "\n"

	passwords, err := accounts.ParsePasswords([]byte(file), exampleConfig(t))

	c.Assert(err, qt.IsNil)
	alex, ok := passwords.Hash("alex")
	c.Assert(ok, qt.IsTrue)
	c.Assert(bcrypt.CompareHashAndPassword(alex, []byte("alex-secret")), qt.IsNil)
	c.Assert(bcrypt.CompareHashAndPassword(alex, []byte("sam-secret")), qt.IsNotNil)
	sam, ok := passwords.Hash("sam")
	c.Assert(ok, qt.IsTrue)
	c.Assert(bcrypt.CompareHashAndPassword(sam, []byte("sam-secret")), qt.IsNil)
	_, ok = passwords.Hash("nobody")
	c.Assert(ok, qt.IsFalse)
}

func TestEachPasswordsRefusal(t *testing.T) {
	t.Parallel()
	lowCost, err := bcrypt.GenerateFromPassword([]byte("cheap"), bcrypt.MinCost)
	qt.New(t).Assert(err, qt.IsNil)
	samLine := "sam:" + hashOf(t, "sam-secret") + "\n"
	tests := []struct {
		name string
		// line follows alex's, and after follows line: sam's own line, in
		// rows where line is not sam's. The secret, line's hash or the line
		// itself when it has no colon, must not appear in the refusal.
		line, after, secret string
		want                string
	}{
		{name: "a line without a colon", line: "sam-secret", secret: "sam-secret", after: samLine,
			want: "line 2: is not name:hash"},
		{name: "a name that is no account ID", line: "Sam Smith:x", secret: ":x", after: samLine,
			want: "line 2: names no account; a name is an account ID"},
		{name: "an account the configuration lacks", line: "kim:" + hashOf(t, "sam-secret"), secret: hashOf(t, "sam-secret"), after: samLine,
			want: "line 2: names kim, which is not an account in the configuration"},
		{name: "an account twice", line: "alex:" + hashOf(t, "sam-secret"), secret: hashOf(t, "sam-secret"), after: samLine,
			want: "line 2: repeats the password of alex from line 1"},
		{name: "Apache MD5", line: "sam:$apr1$r31.....$HqJZimcKQFAMYayBlzkrA/", secret: "HqJZimcKQFAMYayBlzkrA/", // #nosec G101 -- a hash of a throwaway password.
			want: "line 2: the hash for sam is MD5; only bcrypt is accepted"},
		{name: "MD5-crypt", line: "sam:$1$saltsalt$qjXMvbEw8oaL.CzflDugX/", secret: "qjXMvbEw8oaL.CzflDugX/", // #nosec G101 -- a hash of a throwaway password.
			want: "line 2: the hash for sam is MD5; only bcrypt is accepted"},
		{name: "SHA-1", line: "sam:{SHA}W6ph5Mm5Pz8GgiULbPgzG37mj9g=", secret: "W6ph5Mm5Pz8GgiULbPgzG37mj9g=", // #nosec G101 -- a hash of a throwaway password.
			want: "line 2: the hash for sam is SHA-1; only bcrypt is accepted"},
		{name: "SHA-256 crypt", line: "sam:$5$saltsalt$5B8vYYiY.CVt1RlTTf8KbXBH3hsxY/GNooZF2gBfZ49", secret: "5B8vYYiY", // #nosec G101 -- a hash of a throwaway password.
			want: "line 2: the hash for sam is SHA-crypt; only bcrypt is accepted"},
		{name: "SHA-512 crypt", line: "sam:$6$saltsalt$qFmFH.bQmmtXzyBY0s9v7Oicd2z4XSIecDzlB5KiA2", secret: "qFmFH",
			want: "line 2: the hash for sam is SHA-crypt; only bcrypt is accepted"},
		{name: "an early bcrypt variant", line: "sam:$2$10$" + strings.Repeat("a", 53), secret: strings.Repeat("a", 53),
			want: "line 2: the hash for sam is a bcrypt variant other than $2a$, $2b$ and $2y$"},
		{name: "DES crypt", line: "sam:rl0uE2ztN0R5o", secret: "rl0uE2ztN0R5o", // #nosec G101 -- a hash of a throwaway password.
			want: "line 2: the hash for sam is crypt or plain text; only bcrypt is accepted"},
		{name: "a password in plain text", line: "sam:sam-secret", secret: "sam-secret",
			want: "line 2: the hash for sam is crypt or plain text; only bcrypt is accepted"},
		{name: "bcrypt below the lowest cost", line: "sam:" + string(lowCost), secret: string(lowCost),
			want: "line 2: the hash for sam has bcrypt cost 4; the lowest accepted is 10"},
		{name: "bcrypt cut short", line: "sam:" + hashOf(t, "sam-secret")[:59], secret: hashOf(t, "sam-secret")[:59],
			want: "line 2: the hash for sam is not a whole bcrypt hash"},
		{name: "an account with no line", line: "# sam has none yet", secret: "#",
			want: "accounts[1] (sam) has no password"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)
			file := "alex:" + hashOf(t, "alex-secret") + "\n" + test.line + "\n" + test.after

			passwords, err := accounts.ParsePasswords([]byte(file), exampleConfig(t))

			c.Assert(passwords, qt.IsNil)
			invalid := new(accounts.InvalidError)
			c.Assert(err, qt.ErrorAs, &invalid)
			c.Assert(invalid.Problems, qt.DeepEquals, []string{test.want})
			c.Assert(err.Error(), qt.Not(qt.Contains), test.secret)
		})
	}
}

func TestAHashIsMadeOnlyFromAPasswordBcryptReadsWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		password string
		cost     int
		want     string
	}{
		{name: "empty", password: "", cost: accounts.MinCost, want: "accounts: the password is empty"},
		{name: "one byte past what bcrypt reads", password: strings.Repeat("p", 73), cost: accounts.MinCost,
			want: "accounts: the password is longer than the 72 bytes bcrypt reads"},
		{name: "below the lowest cost", password: "secret", cost: 9, want: "accounts: cost 9 is outside 10 to 31"},
		{name: "above bcrypt's highest", password: "secret", cost: 32, want: "accounts: cost 32 is outside 10 to 31"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			hash, err := accounts.Hash([]byte(test.password), test.cost)

			c.Assert(hash, qt.IsNil)
			c.Assert(err, qt.ErrorMatches, test.want)
		})
	}
}

func TestTheLongestPasswordIsHashedWhole(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	longest := []byte(strings.Repeat("p", accounts.MaxPasswordBytes))

	hash, err := accounts.Hash(longest, accounts.MinCost)

	c.Assert(err, qt.IsNil)
	c.Assert(bcrypt.CompareHashAndPassword(hash, longest), qt.IsNil)
	cost, err := bcrypt.Cost(hash)
	c.Assert(err, qt.IsNil)
	c.Assert(cost, qt.Equals, accounts.MinCost)
	c.Assert(accounts.CheckPassword(append(longest, 'p')), qt.ErrorIs, accounts.ErrPasswordTooLong)
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	qt.New(t).Assert(os.WriteFile(path, []byte(content), 0o600), qt.IsNil)
	return path
}

func TestLoadReadsBothFilesAndNamesTheOneRefused(t *testing.T) {
	t.Parallel()
	valid := "alex:" + hashOf(t, "alex-secret") + "\nsam:" + hashOf(t, "sam-secret") + "\n"
	tests := []struct {
		name               string
		config, passwords  string
		wantConfig, wantPw string
		want               string
	}{
		{name: "a configuration too large to be one", config: strings.Repeat("#", 1<<20+1), passwords: valid,
			want: `.*/configuration.yaml: larger than 1048576 bytes`},
		{name: "a refused configuration", config: "kind: HostedConfiguration\n", passwords: valid,
			want: `(?s).*/configuration.yaml: refused:\n  - apiVersion: must be console.ptah.run/v1alpha1.*`},
		{name: "refused passwords", config: string(example(t)), passwords: "alex:x\n",
			want: `(?s).*/passwords: refused:\n  - line 1: the hash for alex is crypt or plain text.*`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			config, passwords, err := accounts.Load(
				writeFile(t, "configuration.yaml", test.config), writeFile(t, "passwords", test.passwords))

			c.Assert(config, qt.IsNil)
			c.Assert(passwords, qt.IsNil)
			c.Assert(err, qt.ErrorMatches, test.want)
		})
	}
}

func TestLoadAcceptsAValidPair(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	valid := "alex:" + hashOf(t, "alex-secret") + "\nsam:" + hashOf(t, "sam-secret") + "\n"

	config, passwords, err := accounts.Load(
		writeFile(t, "configuration.yaml", string(example(t))), writeFile(t, "passwords", valid))

	c.Assert(err, qt.IsNil)
	c.Assert(config.InstallationID, qt.Equals, "prod-eu-1")
	_, ok := passwords.Hash("sam")
	c.Assert(ok, qt.IsTrue)
}

func TestLoadNeedsBothFiles(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	_, _, err := accounts.Load(filepath.Join(t.TempDir(), "absent.yaml"), filepath.Join(t.TempDir(), "absent"))

	c.Assert(err, qt.ErrorIs, os.ErrNotExist)
}
