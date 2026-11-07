// Command secretscan refuses an acceptance artifact that holds a password the
// lab made. Every password the rows type is a fact whose name ends in
// _PASSWORD (hack/e2e/lib.sh writes them, web/e2e/lab.ts reads them).
// secretscan looks for each one in the files it is given: inside zip archives,
// which is how Playwright stores a trace and how the HTML report keeps its
// copies, inside gzip streams, and inside the base64 data the HTML report
// embeds its steps in. It also looks for the forms a request body or a JSON
// document gives a value: URL-encoded, JSON-escaped and base64.
//
//	go run ./hack/e2e/secretscan -facts "$OUT_DIR/facts" film web/test-results web/playwright-report
//
// It names the file and the fact, never the value, and exits 1 when it finds
// one or when it cannot read something it was given.
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// minLength is the shortest password secretscan looks for. A shorter
	// value, and its base64 forms, would turn up in a video by chance.
	minLength = 12
	// maxDepth bounds how far archives and encoded data nest: a trace in a
	// zip is two levels, the report's steps in base64 in HTML four.
	maxDepth = 6
	// maxEntry bounds what one archive entry or stream may inflate to.
	maxEntry = 1 << 30
)

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// run is the command: it returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("secretscan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	factsPath := flags.String("facts", "", "the facts file the lab wrote")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *factsPath == "" || flags.NArg() == 0 {
		fmt.Fprintln(stderr, "usage: secretscan -facts FILE PATH...")
		return 2
	}
	secrets, err := readFacts(*factsPath)
	if err != nil {
		fmt.Fprintln(stderr, "secretscan:", err)
		return 1
	}
	s := &scan{secrets: secrets}
	out := &redacting{w: stdout, secrets: secrets}
	if len(secrets) == 0 {
		fmt.Fprintf(out, "secretscan: %s records no password, so nothing the rows typed can be in the artifact\n", *factsPath)
		return 0
	}
	for _, root := range flags.Args() {
		if err := s.walk(root, out); err != nil {
			fmt.Fprintln(&redacting{w: stderr, secrets: secrets}, "secretscan:", err)
			return 1
		}
	}
	if s.files == 0 {
		fmt.Fprintln(stderr, "secretscan: no file under the paths given; refusing to report a clean artifact")
		return 1
	}
	for _, f := range s.findings {
		fmt.Fprintf(out, "%s: holds %s, %s\n", f.where, f.fact, f.encoding)
	}
	fmt.Fprintf(out, "secretscan: looked for %d passwords in %d files and %d archive entries and decoded streams: %s\n",
		len(secrets), s.files, s.decoded, found(len(s.findings)))
	if len(s.findings) > 0 {
		return 1
	}
	return 0
}

func found(n int) string {
	if n == 0 {
		return "none found"
	}
	return fmt.Sprintf("found %d", n)
}

// secret is one password and the forms it is looked for in.
type secret struct {
	fact    string
	needles []needle
}

type needle struct {
	encoding string
	value    []byte
}

// readFacts returns the passwords a facts file records. A file that does not
// exist records none: the lab writes it before any row runs, and no password
// exists before hosted.sh or oidc.sh makes one.
func readFacts(path string) ([]secret, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var secrets []secret
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || name == "" {
			// The line itself is not printed: it may be a value.
			return nil, fmt.Errorf("line %d of %s is not NAME=value", i+1, path)
		}
		if !strings.HasSuffix(name, "_PASSWORD") {
			continue
		}
		if len(value) < minLength {
			return nil, fmt.Errorf("%s in %s is %d bytes long; secretscan looks for passwords of at least %d",
				name, path, len(value), minLength)
		}
		secrets = append(secrets, secret{fact: name, needles: needles(value)})
	}
	return secrets, nil
}

// needles is every form of a value secretscan looks for.
func needles(value string) []needle {
	var out []needle
	seen := make(map[string]bool)
	add := func(encoding, form string) {
		if !seen[form] {
			seen[form] = true
			out = append(out, needle{encoding: encoding, value: []byte(form)})
		}
	}
	add("as typed", value)
	// A form body or a query: url.QueryEscape is what a browser sends for a
	// form; url.PathEscape leaves "+" alone. Some encoders write lowercase hex.
	for _, form := range []string{url.QueryEscape(value), url.PathEscape(value)} {
		add("URL-encoded", form)
		add("URL-encoded", lowerHex(form))
	}
	// A JSON string: encoding/json escapes <, > and &; other encoders escape
	// "/" as well.
	for _, escapeHTML := range []bool{true, false} {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(escapeHTML)
		_ = encoder.Encode(value) // a string always encodes
		form := strings.TrimSuffix(strings.TrimSpace(buffer.String()), `"`)[1:]
		add("JSON-escaped", form)
		add("JSON-escaped", strings.ReplaceAll(form, "/", `\/`))
	}
	// Base64 of a stream that holds the value: the value starts at one of
	// three offsets within a three-byte group, and only the groups wholly
	// inside it encode the same whatever surrounds them. The URL-safe
	// alphabet differs only in the characters "+" and "/", which no group of
	// a value spelled in the base64 alphabet encodes to.
	for offset := range 3 {
		stream := append(make([]byte, offset), value...)
		encoded := base64.StdEncoding.EncodeToString(stream)
		first := (offset + 2) / 3
		last := (offset + len(value)) / 3
		add("base64", encoded[4*first:4*last])
	}
	return out
}

var percentHex = regexp.MustCompile(`%[0-9A-F]{2}`)

func lowerHex(s string) string {
	return percentHex.ReplaceAllStringFunc(s, strings.ToLower)
}

type finding struct {
	where, fact, encoding string
}

type scan struct {
	secrets  []secret
	files    int
	decoded  int
	findings []finding
}

// walk examines every regular file under root. A root that does not exist is
// reported and skipped: a run that failed early made no recordings. A link is
// followed, as an upload would follow it, as long as it stays under root; one
// that leaves it is an error.
func (s *scan) walk(root string, out io.Writer) error {
	info, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(out, "secretscan: %s does not exist\n", root)
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(root)
		if err != nil {
			return err
		}
		s.files++
		return s.look(root, data, 0)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }() // read only
	return fs.WalkDir(dir.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := dir.Stat(name)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(root, name), err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := dir.ReadFile(name)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(root, name), err)
		}
		s.files++
		return s.look(filepath.Join(root, filepath.FromSlash(name)), data, 0)
	})
}

// base64Data is a data URL's payload, as the HTML report embeds its steps.
var base64Data = regexp.MustCompile(`base64,([A-Za-z0-9+/]{16,}={0,2})`)

// look searches data for every needle, then whatever data holds: a zip
// archive's entries, a gzip stream, base64 data.
func (s *scan) look(where string, data []byte, depth int) error {
	for _, secret := range s.secrets {
		for _, needle := range secret.needles {
			if bytes.Contains(data, needle.value) {
				s.findings = append(s.findings, finding{where: where, fact: secret.fact, encoding: needle.encoding})
			}
		}
	}
	isZip := bytes.HasPrefix(data, []byte("PK\x03\x04")) || bytes.HasPrefix(data, []byte("PK\x05\x06"))
	isGzip := bytes.HasPrefix(data, []byte{0x1f, 0x8b})
	matches := base64Data.FindAllSubmatchIndex(data, -1)
	if !isZip && !isGzip && len(matches) == 0 {
		return nil
	}
	if depth >= maxDepth {
		return fmt.Errorf("%s: nested deeper than %d levels; cannot examine it", where, maxDepth)
	}
	var err error
	switch {
	case isZip:
		err = s.lookInZip(where, data, depth)
	case isGzip:
		err = s.lookInGzip(where, data, depth)
	}
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := s.lookInBase64(where, data[match[2]:match[3]], match[2], depth); err != nil {
			return err
		}
	}
	return nil
}

func (s *scan) lookInZip(where string, data []byte, depth int) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return fmt.Errorf("%s!%s: %w", where, file.Name, err)
		}
		content, err := readBounded(reader)
		_ = reader.Close() // read to the end or failed; the error is err
		if err != nil {
			return fmt.Errorf("%s!%s: %w", where, file.Name, err)
		}
		s.decoded++
		if err := s.look(where+"!"+file.Name, content, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (s *scan) lookInGzip(where string, data []byte, depth int) error {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	content, err := readBounded(reader)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	s.decoded++
	return s.look(where+"#gzip", content, depth+1)
}

func (s *scan) lookInBase64(where string, payload []byte, offset, depth int) error {
	// Decoded without its padding, a payload of any length but one past a
	// whole group is valid; the regexp admits no "=" before the end.
	payload = bytes.TrimRight(payload, "=")
	if len(payload)%4 == 1 {
		payload = payload[:len(payload)-1]
	}
	content := make([]byte, base64.RawStdEncoding.DecodedLen(len(payload)))
	n, err := base64.RawStdEncoding.Decode(content, payload)
	if err != nil {
		return fmt.Errorf("%s: base64 data at byte %d: %w", where, offset, err)
	}
	s.decoded++
	return s.look(fmt.Sprintf("%s#base64@%d", where, offset), content[:n], depth+1)
}

func readBounded(reader io.Reader) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxEntry+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxEntry {
		return nil, fmt.Errorf("inflates past %d bytes; cannot examine it", maxEntry)
	}
	return content, nil
}

// redacting writes through to w with every form of every password replaced
// by the name of its fact, so nothing secretscan prints can carry one, a
// path included.
type redacting struct {
	w       io.Writer
	secrets []secret
}

func (r *redacting) Write(p []byte) (int, error) {
	out := p
	for _, secret := range r.secrets {
		for _, needle := range secret.needles {
			out = bytes.ReplaceAll(out, needle.value, []byte("["+secret.fact+"]"))
		}
	}
	if _, err := r.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
