// Package diagnostics_test runs hack/e2e/diagnostics.sh against a kubectl and
// a docker that answer from a table. What it proves is the script's own logic:
// which cluster each file was read from, and what is kept when the second
// cluster is not there. What a real cluster answers is the Acceptance
// workflow's to show.
package diagnostics_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	qt "github.com/frankban/quicktest"
)

const script = "../../hack/e2e/diagnostics.sh"

// stubKubectl records each call and answers with the kubeconfig it was given,
// so a kept file says which cluster it came from.
const stubKubectl = `#!/bin/sh
printf '%s\n' "$*" >>"$STUB_LOG"
from=$(basename "$2")
[ "$2" != "$STUB_FROZEN_KUBECONFIG" ] || {
	echo 'Unable to connect to the server: net/http: TLS handshake timeout' >&2
	exit 1
}
case "$*" in
*"get pods -l app.kubernetes.io/managed-by=ptah-operator"*) echo "ptah-plan-$from" ;;
*) echo "from $from" ;;
esac
`

const stubDocker = `#!/bin/sh
echo paused
`

// shared is what the script keeps of either cluster, apart from the Pod's.
var shared = []string{
	"resources.yaml", "pods.txt", "nodes.txt", "events.txt", "events-kube-system.txt",
	"postgres.log", "coredns.log", "operator.log",
}

// secondCluster is the state a run leaves the second cluster in.
type secondCluster int

const (
	neverCreated secondCluster = iota
	frozen
	answering
)

type run struct {
	dir, log, secondKubeconfig string
}

// runScript runs the script in a lab of its own and returns where it looked.
func runScript(t *testing.T, second secondCluster) run {
	t.Helper()
	c := qt.New(t)
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	c.Assert(os.Mkdir(bin, 0o755), qt.IsNil)
	c.Assert(os.WriteFile(filepath.Join(bin, "kubectl"), []byte(stubKubectl), 0o755), qt.IsNil) // #nosec G306 -- a stub has to be executable.
	c.Assert(os.WriteFile(filepath.Join(bin, "docker"), []byte(stubDocker), 0o755), qt.IsNil)   // #nosec G306 -- as above.

	out := filepath.Join(root, "out")
	secondKubeconfig := filepath.Join(out, "target", "kubeconfig")
	if second != neverCreated {
		c.Assert(os.MkdirAll(filepath.Dir(secondKubeconfig), 0o755), qt.IsNil)
		c.Assert(os.WriteFile(secondKubeconfig, []byte("second"), 0o600), qt.IsNil)
	}
	environment := filepath.Join(root, "environment")
	c.Assert(os.WriteFile(environment, []byte(strings.Join([]string{
		"E2E_TEST_NAMESPACE=lab-namespace",
		"E2E_CONTROLLER_NAME=controller",
		"E2E_OPERATOR_NAMESPACE=operator-namespace",
		"E2E_KIND_CLUSTER_NAME=lab",
		"E2E_KUBECONFIG=" + filepath.Join(root, "lab.kubeconfig"),
		"E2E_DOCKER_CONTEXT=lab-context",
		"E2E_DOCKER_CONFIG=" + filepath.Join(root, "docker"),
	}, "\n")+"\n"), 0o600), qt.IsNil)

	result := run{
		dir:              filepath.Join(root, "kept"),
		log:              filepath.Join(root, "calls.log"),
		secondKubeconfig: secondKubeconfig,
	}
	cmd := exec.CommandContext(t.Context(), "sh", script, result.dir) // #nosec G204 -- this repository's script and a test's temporary directory.
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"LAB_ENVIRONMENT="+environment,
		"OPERATOR_DIR="+root,
		"OUT_DIR="+out,
		"STUB_LOG="+result.log,
	)
	if second == frozen {
		cmd.Env = append(cmd.Env, "STUB_FROZEN_KUBECONFIG="+secondKubeconfig)
	}
	output, err := cmd.CombinedOutput()
	c.Assert(err, qt.IsNil, qt.Commentf("%s", output))
	return result
}

func (r run) calls(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(r.log)
	qt.New(t).Assert(err, qt.IsNil)
	return strings.Split(strings.TrimSpace(string(body)), "\n")
}

// missing is what the script named in missing.txt, if it wrote one.
func (r run) missing() string {
	body, err := os.ReadFile(filepath.Join(r.dir, "missing.txt"))
	if err != nil {
		return ""
	}
	return string(body)
}

// secondReads is the calls made with the second cluster's kubeconfig.
func (r run) secondReads(t *testing.T) []string {
	t.Helper()
	var reads []string
	for _, call := range r.calls(t) {
		if strings.Contains(call, r.secondKubeconfig) {
			reads = append(reads, call)
		}
	}
	return reads
}

// A kept file says which cluster it was read from: the first read cannot pass
// for the second, whatever else the two accounts hold.
func assertAccount(t *testing.T, dir, from string, parts ...string) {
	t.Helper()
	c := qt.New(t)
	c.Assert(parts, qt.Not(qt.HasLen), 0)
	for _, part := range parts {
		body, err := os.ReadFile(filepath.Join(dir, part))
		c.Assert(err, qt.IsNil)
		c.Assert(strings.TrimSpace(string(body)), qt.Equals, "from "+from, qt.Commentf("%s", part))
	}
}

// The script runs after a failure of any kind. The lab's account is kept
// whatever the second cluster's state, and the script does not fail on it.
func TestTheLabsAccountIsKept(t *testing.T) {
	const lab = "lab.kubeconfig"
	rows := []struct {
		name        string
		second      secondCluster
		wantMissing string
	}{
		{name: "the second cluster was never created", second: neverCreated, wantMissing: "second-cluster: no kubeconfig at .*\n"},
		{
			name: "the second cluster does not answer", second: frozen,
			wantMissing: "second-cluster: its API server did not answer; the reason is in second-cluster/reachable.txt, its node is paused\n",
		},
		{name: "the second cluster answers", second: answering},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			kept := runScript(t, row.second)

			assertAccount(t, kept.dir, lab, shared...)
			assertAccount(t, kept.dir, lab, "probe.log",
				"operations/ptah-plan-lab.kubeconfig.log", "operations/ptah-plan-lab.kubeconfig.status.json")

			qt.New(t).Assert(kept.missing(), qt.Matches, row.wantMissing)
		})
	}
}

// The second cluster gets the same account, under a path of its own, read
// with the kubeconfig external.sh reaches it with. The probe runs on the lab
// alone, so its log is not expected there and is not reported missing.
func TestTheSecondClustersAccountIsKeptApart(t *testing.T) {
	c := qt.New(t)
	kept := runScript(t, answering)
	const second = "kubeconfig"

	at := filepath.Join(kept.dir, "second-cluster")
	assertAccount(t, at, second, shared...)
	assertAccount(t, at, second, "operations/ptah-plan-kubeconfig.log", "operations/ptah-plan-kubeconfig.status.json")
	_, err := os.Stat(filepath.Join(at, "probe.log"))
	c.Assert(os.IsNotExist(err), qt.IsTrue)

	// One read to decide, the shared parts, the Pod list and the Pod's two.
	c.Assert(kept.secondReads(t), qt.HasLen, 1+len(shared)+1+2)
}

// A node a failed row left frozen answers nothing, so one read decides. Going
// on through every part would wait out every part's bound for no more news.
func TestAFrozenSecondClusterCostsOneRead(t *testing.T) {
	c := qt.New(t)
	kept := runScript(t, frozen)

	reads := kept.secondReads(t)
	c.Assert(reads, qt.HasLen, 1)
	c.Assert(reads[0], qt.Contains, "--request-timeout=20s")

	said, err := os.ReadFile(filepath.Join(kept.dir, "second-cluster", "reachable.txt"))
	c.Assert(err, qt.IsNil)
	c.Assert(string(said), qt.Contains, "TLS handshake timeout")
}

// No read may hang the step that uploads what was kept, and none may touch a
// Secret. The count is held so a change of what the log records cannot pass
// this over nothing.
func TestEveryReadIsBoundedAndNoneReadsASecret(t *testing.T) {
	c := qt.New(t)
	kept := runScript(t, answering)

	calls := kept.calls(t)
	c.Assert(len(calls) > 2*len(shared), qt.IsTrue, qt.Commentf("%d calls", len(calls)))
	for _, call := range calls {
		// The first two words name a kubeconfig under the test's own directory.
		read := strings.Join(strings.Fields(call)[2:], " ")
		c.Assert(read, qt.Contains, "--request-timeout=")
		c.Assert(strings.ToLower(read), qt.Not(qt.Contains), "secret")
	}
}
