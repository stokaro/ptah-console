package main

// White-box testing required: a command is package main, which no other
// package imports. The tests drive run, the command with its arguments and
// its output, and read needles to hold the output to printing no password.

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
)

// The planted passwords have the shape `openssl rand -base64 18` gives the
// lab's: 24 characters, with "+" and "/" among them.
const (
	authorPassword   = "pl4nted+Author/Secret9xQ"
	approverPassword = "pl4nted+Approver/Secret7"
	oldPassword      = "pl4nted+OldApprover/Sec5"
	oidcPassword     = "pl4nted+Oidc/SecretVal3W"
)

var planted = []string{authorPassword, approverPassword, oldPassword, oidcPassword}

// facts is what the lab records after the hosted and OIDC rows: the
// passwords, and facts beside them that are not passwords, some named close
// to one.
const facts = "NAMESPACE=e2e\n" +
	"HOSTED_URL=https://localhost:18443\n" +
	"HOSTED_AUTHOR=ptah-console:e2e:author\n" +
	"HOSTED_AUTHOR_PASSWORD=" + authorPassword + "\n" +
	"HOSTED_APPROVER=ptah-console:e2e:approver\n" +
	"HOSTED_APPROVER_PASSWORD=" + approverPassword + "\n" +
	"HOSTED_APPROVER_OLD_PASSWORD=" + oldPassword + "\n" +
	"HOSTED_AUTHOR_PASSWORD_FILE=/runner/e2e/hosted-author.password\n" +
	"PASSWORDS_SECRET=ptah-console-passwords-secret\n" +
	"OIDC_EMAIL=approver@e2e.test\n" +
	"OIDC_USERNAME=oidc:approver@e2e.test\n" +
	"OIDC_PASSWORD=" + oidcPassword + "\n"

// The artifact's paths, as the Acceptance workflow uploads them.
var roots = []string{"film", "web/test-results", "web/playwright-report"}

// fillStep is the line test.trace writes for locator.fill, the Fill step the
// HTML report and the trace viewer show.
func fillStep(value string) string {
	return fmt.Sprintf(`{"type":"before","callId":"wdkt@58","startTime":1645.53,"class":"Test","method":"pw:api",`+
		`"title":"Fill \"%s\"","subtitle":"getByRole('form', { name: 'Sign in' }).getByLabel('Password')",`+
		`"params":{"locator":"getByRole('form', { name: 'Sign in' }).getByLabel('Password')","value":"%s"}}`+"\n",
		value, value)
}

// snapshot is a DOM snapshot line of 1-trace.trace: the snapshotter keeps
// every input's value, a password field's included.
func snapshot(value string) string {
	return fmt.Sprintf(`{"type":"frame-snapshot","snapshot":{"callId":"wdkt@60","snapshotName":"before@wdkt@60",`+
		`"frameUrl":"https://localhost:18443/login","html":["FORM",{"aria-label":"Sign in"},`+
		`["INPUT",{"type":"password","name":"password","__playwright_value_":"%s"}]]}}`+"\n", value)
}

// formBody is a form's request body as a trace keeps it in resources/.
func formBody(password string) string {
	return "login=approver%40e2e.test&password=" + url.QueryEscape(password)
}

type entry struct{ name, content string }

// must stops a fixture that could not be built. The fixtures write into
// memory, which does not fail.
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// zipped is a zip archive of entries, deflated as Playwright writes a trace.
func zipped(entries ...entry) string {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, e := range entries {
		w, err := archive.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate})
		must(err)
		_, err = w.Write([]byte(e.content))
		must(err)
	}
	must(archive.Close())
	return buffer.String()
}

func gzipped(content string) string {
	var buffer bytes.Buffer
	w := gzip.NewWriter(&buffer)
	_, err := w.Write([]byte(content))
	must(err)
	must(w.Close())
	return buffer.String()
}

// nested is a zip archive in a zip archive, levels deep.
func nested(levels int) string {
	content := "nothing"
	for range levels {
		content = zipped(entry{"inner.zip", content})
	}
	return content
}

// report is the HTML report's index.html: its steps in a zip, in base64, in
// a template element after an inline image of its own.
func report(steps string) string {
	image := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n not a password, only an image"))
	data := base64.StdEncoding.EncodeToString([]byte(zipped(entry{"report.json", `{"files":[]}`},
		entry{"81228c4c8886d566ba11.json", steps})))
	return `<!DOCTYPE html><html><head><style>.logo{background:url(data:image/png;base64,` + image +
		`)}</style></head><body><div id='root'></div></body></html>` + "\n" +
		`<template id="playwrightReportBase64">data:application/zip;base64,` + data + `</template>`
}

func b64(prefix, value string) string {
	return base64.StdEncoding.EncodeToString([]byte(prefix + value))
}

// nearMisses is each password with its middle character changed, in every
// form secretscan looks for. Every form covers the middle character.
func nearMisses() string {
	var content strings.Builder
	for _, value := range planted {
		middle := len(value) / 2
		near := value[:middle] + "Z" + value[middle+1:]
		for _, form := range []string{near, url.QueryEscape(near), strings.ReplaceAll(near, "/", `\/`),
			b64("", near), b64("a", near), b64("ab", near)} {
			content.WriteString(form + "\n")
		}
	}
	return content.String()
}

// prefixes is each password without its last character, as typed and
// URL-encoded. Base64 cannot see the first two and the last two bytes of a
// value, so no base64 form is planted.
func prefixes() string {
	var content strings.Builder
	for _, value := range planted {
		prefix := value[:len(value)-1]
		content.WriteString(prefix + "\n" + url.QueryEscape(prefix) + "\n")
	}
	return content.String()
}

// otherFacts is the value of every fact that is not a password.
func otherFacts() string {
	var content strings.Builder
	for line := range strings.SplitSeq(facts, "\n") {
		name, value, _ := strings.Cut(line, "=")
		if !strings.HasSuffix(name, "_PASSWORD") {
			content.WriteString(value + "\n")
		}
	}
	return content.String()
}

// noFacts stands for a facts file that was never written.
const noFacts = "\x00no facts file"

// scanned writes the facts and the files under a directory of their own and
// runs the command over the artifact's paths there.
func scanned(c *qt.C, factsText string, files map[string]string) (code int, stdout, stderr string) {
	dir := c.TempDir()
	factsPath := filepath.Join(dir, "facts")
	if factsText != noFacts {
		c.Assert(os.WriteFile(factsPath, []byte(factsText), 0o600), qt.IsNil)
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		c.Assert(os.MkdirAll(filepath.Dir(path), 0o755), qt.IsNil)
		c.Assert(os.WriteFile(path, []byte(content), 0o600), qt.IsNil)
	}
	args := []string{"-facts", factsPath}
	for _, root := range roots {
		args = append(args, filepath.Join(dir, filepath.FromSlash(root)))
	}
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// printsNoPassword holds the output to the rule that it never carries a
// password, in any form secretscan knows.
func printsNoPassword(c *qt.C, output string, values ...string) {
	c.Helper()
	for _, value := range values {
		for _, n := range needles(value) {
			c.Assert(output, qt.Not(qt.Contains), string(n.value), qt.Commentf("the output holds a password, %s", n.encoding))
		}
	}
}

func TestRefusesAnArtifactThatHoldsAPassword(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files map[string]string
		// says are the lines of stdout that name each finding, by path under
		// the artifact, as regular expressions.
		says []string
	}{{
		name: "a Fill step in a trace",
		files: map[string]string{"web/test-results/hosted-sign-in/trace.zip": zipped(
			entry{"test.trace", fillStep(authorPassword)}, entry{"1-trace.trace", snapshot("")})},
		says: []string{`web/test-results/hosted-sign-in/trace.zip!test.trace: holds HOSTED_AUTHOR_PASSWORD, as typed`},
	}, {
		name: "a DOM snapshot of the password field",
		files: map[string]string{"web/test-results/hosted-sign-in/trace.zip": zipped(
			entry{"test.trace", fillStep("placeholder")}, entry{"1-trace.trace", snapshot(approverPassword)})},
		says: []string{`trace.zip!1-trace.trace: holds HOSTED_APPROVER_PASSWORD, as typed`},
	}, {
		name: "a form's request body in a trace's resources",
		files: map[string]string{"web/test-results/oidc/trace.zip": zipped(
			entry{"resources/923de40a0b89.dat", formBody(oidcPassword)})},
		says: []string{`trace.zip!resources/923de40a0b89.dat: holds OIDC_PASSWORD, URL-encoded`},
	}, {
		name: "the steps the HTML report embeds",
		files: map[string]string{"web/playwright-report/index.html": report(
			`{"steps":[{"title":"Fill \"` + oldPassword + `\"","subtitle":"getByLabel('Password')"}]}`)},
		says: []string{`web/playwright-report/index.html#base64@\d+!81228c4c8886d566ba11.json: holds HOSTED_APPROVER_OLD_PASSWORD, as typed`},
	}, {
		name: "a trace the HTML report keeps a copy of",
		files: map[string]string{"web/playwright-report/data/5d92444459ed.zip": zipped(
			entry{"test.trace", fillStep(approverPassword)})},
		says: []string{`web/playwright-report/data/5d92444459ed.zip!test.trace: holds HOSTED_APPROVER_PASSWORD, as typed`},
	}, {
		name: "JSON that escapes the slash",
		files: map[string]string{"film/diagnostics/events.json": `{"note":"` +
			strings.ReplaceAll(approverPassword, "/", `\/`) + `"}`},
		says: []string{`film/diagnostics/events.json: holds HOSTED_APPROVER_PASSWORD, JSON-escaped`},
	}, {
		name:  "a form body in lowercase hex",
		files: map[string]string{"film/diagnostics/requests.log": lowerHex(formBody(authorPassword))},
		says:  []string{`film/diagnostics/requests.log: holds HOSTED_AUTHOR_PASSWORD, URL-encoded`},
	}, {
		name: "a Basic authorization header",
		files: map[string]string{"film/diagnostics/requests.log": "Authorization: Basic " +
			b64("approver@e2e.test:", approverPassword)},
		says: []string{`film/diagnostics/requests.log: holds HOSTED_APPROVER_PASSWORD, base64`},
	}, {
		name:  "base64 that starts one byte into a group",
		files: map[string]string{"film/diagnostics/a.txt": b64("a", oidcPassword)},
		says:  []string{`film/diagnostics/a.txt: holds OIDC_PASSWORD, base64`},
	}, {
		name:  "base64 that starts two bytes into a group",
		files: map[string]string{"film/diagnostics/ab.txt": b64("ab", oidcPassword)},
		says:  []string{`film/diagnostics/ab.txt: holds OIDC_PASSWORD, base64`},
	}, {
		name:  "a gzip stream",
		files: map[string]string{"film/diagnostics/console.log.gz": gzipped("password=" + approverPassword + "\n")},
		says:  []string{`film/diagnostics/console.log.gz#gzip: holds HOSTED_APPROVER_PASSWORD, as typed`},
	}, {
		// The planted password holds a "/", so its two halves name a
		// directory and a file.
		name:  "a path that spells a password, printed without it",
		files: map[string]string{"film/" + authorPassword + ".txt": authorPassword},
		says:  []string{`film/\[HOSTED_AUTHOR_PASSWORD\]\.txt: holds HOSTED_AUTHOR_PASSWORD, as typed`},
	}, {
		name:  "every password the facts record, each found",
		files: map[string]string{"film/all.txt": strings.Join(planted, " ")},
		says: []string{
			`film/all.txt: holds HOSTED_AUTHOR_PASSWORD, as typed`,
			`film/all.txt: holds HOSTED_APPROVER_PASSWORD, as typed`,
			`film/all.txt: holds HOSTED_APPROVER_OLD_PASSWORD, as typed`,
			`film/all.txt: holds OIDC_PASSWORD, as typed`,
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, stdout, stderr := scanned(c, facts, test.files)

			c.Assert(code, qt.Equals, 1, qt.Commentf("stdout: %s\nstderr: %s", stdout, stderr))
			for _, line := range test.says {
				c.Assert(stdout, qt.Matches, `(?s).*`+line+`\n.*`, qt.Commentf("stderr: %s", stderr))
			}
			c.Assert(stdout, qt.Matches, `(?s).*secretscan: looked for 4 passwords in 1 files and \d+ archive entries and decoded streams: found \d+\n`)
			printsNoPassword(c, stdout+stderr, planted...)
		})
	}
}

func TestPassesAnArtifactWithoutOne(t *testing.T) {
	t.Parallel()
	const placeholder = "placeholder, not the password"
	tests := []struct {
		name  string
		files map[string]string
		says  string
	}{{
		name: "a sign-in whose form got a placeholder",
		files: map[string]string{
			"web/test-results/hosted-sign-in/trace.zip": zipped(
				entry{"test.trace", fillStep(placeholder)},
				entry{"1-trace.trace", snapshot(placeholder)},
				entry{"resources/6c0e260c728a.json", `{"login":"approver@e2e.test","password":"` + placeholder + `"}`}),
			"web/test-results/hosted-sign-in/video.webm": "\x1a\x45\xdf\xa3 not a password",
			"web/playwright-report/index.html":           report(`{"steps":[{"title":"Fill \"` + placeholder + `\""}]}`),
		},
		says: `looked for 4 passwords in 3 files and 7 archive entries and decoded streams: none found`,
	}, {
		name:  "each password with one character changed, in every form",
		files: map[string]string{"film/near.txt": nearMisses()},
		says:  `looked for 4 passwords in 1 files and 0 archive entries and decoded streams: none found`,
	}, {
		name:  "each password without its last character",
		files: map[string]string{"film/prefix.txt": prefixes()},
		says:  `looked for 4 passwords in 1 files and 0 archive entries and decoded streams: none found`,
	}, {
		name:  "the values of facts that are not passwords",
		files: map[string]string{"film/facts.txt": otherFacts()},
		says:  `looked for 4 passwords in 1 files and 0 archive entries and decoded streams: none found`,
	}, {
		name:  "a path that does not exist",
		files: map[string]string{"film/versions.txt": "operator v0.2.9\n"},
		says:  `(?s).*web/test-results does not exist\n.*looked for 4 passwords in 1 files and 0 archive entries and decoded streams: none found`,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, stdout, stderr := scanned(c, facts, test.files)

			c.Assert(code, qt.Equals, 0, qt.Commentf("stdout: %s\nstderr: %s", stdout, stderr))
			c.Assert(stdout, qt.Matches, `(?s).*`+test.says+`\n`)
			c.Assert(stdout, qt.Not(qt.Matches), `(?s).*: holds .*`)
			printsNoPassword(c, stdout+stderr, planted...)
		})
	}
}

func TestPassesWhenNoPasswordWasMade(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		facts string
	}{
		{name: "facts from the local rows alone", facts: "NAMESPACE=e2e\nAPPROVER=e2e-approver\n"},
		{name: "an empty facts file", facts: ""},
		{name: "no facts file: the lab did not come up", facts: noFacts},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, stdout, _ := scanned(c, test.facts, map[string]string{"film/versions.txt": "operator v0.2.9\n"})

			c.Assert(code, qt.Equals, 0)
			c.Assert(stdout, qt.Matches, `secretscan: .*facts records no password, so nothing the rows typed can be in the artifact\n`)
		})
	}
}

func TestRefusesWhatItCannotExamine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		facts string
		files map[string]string
		says  string
	}{{
		name:  "a zip archive cut short",
		facts: facts,
		files: map[string]string{"web/test-results/row/trace.zip": zipped(entry{"test.trace", fillStep("x")})[:40]},
		says:  `secretscan: .*/web/test-results/row/trace.zip: zip: not a valid zip file\n`,
	}, {
		name:  "archives nested past the bound",
		facts: facts,
		files: map[string]string{"film/nested.zip": nested(maxDepth + 1)},
		says:  `secretscan: .*/film/nested.zip(!inner.zip)+: nested deeper than 6 levels; cannot examine it\n`,
	}, {
		name:  "passwords on record and no file to look in",
		facts: facts,
		files: nil,
		says:  `secretscan: no file under the paths given; refusing to report a clean artifact\n`,
	}, {
		name:  "a password too short to look for",
		facts: "HOSTED_AUTHOR_PASSWORD=pl4nt+Short\n",
		files: map[string]string{"film/a.txt": "a"},
		says:  `secretscan: HOSTED_AUTHOR_PASSWORD in .*/facts is 11 bytes long; secretscan looks for passwords of at least 12\n`,
	}, {
		name:  "a facts line with no name",
		facts: "=" + authorPassword + "\n",
		files: map[string]string{"film/a.txt": "a"},
		says:  `secretscan: line 1 of .*/facts is not NAME=value\n`,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			code, stdout, stderr := scanned(c, test.facts, test.files)

			c.Assert(code, qt.Equals, 1, qt.Commentf("stdout: %s\nstderr: %s", stdout, stderr))
			c.Assert(stderr, qt.Matches, test.says)
			printsNoPassword(c, stdout+stderr, planted...)
		})
	}
}
