package enrollment

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	operatorv1alpha1 "github.com/stokaro/ptah-operator/api/v1alpha1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	agentv1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/agent/v1/agentv1connect"
	"github.com/stokaro/ptah-console/internal/kube"
)

// Defaults of the target side.
const (
	DefaultAgentNamespace = "ptah-console-agent"
	DefaultRelease        = "ptah-console-agent"
	credentialKey         = "credential"
	connectionAnnotation  = "console.ptah.run/connection-uid"
)

// Helper is one run of the target administrator's helper.
type Helper struct {
	File *File
	// Kube is the administrator's own configuration, for the context they
	// named. Nothing of it is sent to the console.
	Kube        *rest.Config
	ContextName string
	// Namespace and Release are where the agent goes.
	Namespace string
	Release   string
	// ValuesPath is where the agent chart's values are written.
	ValuesPath string
	// Confirm shows the summary and says whether the administrator approved
	// it. Nil approves.
	Confirm func(summary string) (bool, error)
	Out     io.Writer
	Now     func() time.Time
	// Transport reaches the console; nil trusts only the file's CA, under
	// the file's server name.
	Transport http.RoundTripper
}

// ErrDeclined is a run the administrator did not approve.
var ErrDeclined = errors.New("enrollment: not approved; nothing was changed")

// Enroll shows the administrator what the enrollment installs and grants,
// and with their approval writes the agent's channel credential into its
// Secret on the target, completes the enrollment on the console, and writes
// the agent chart's values with the Helm command that installs it. A run
// that stopped part way can run again with the same file: it reuses the
// credential it wrote and reports the same completion.
func (h *Helper) Enroll(ctx context.Context) error {
	if h.Now == nil {
		h.Now = time.Now
	}
	if !h.Now().Before(h.File.ExpiresAt) {
		return fmt.Errorf("enrollment: the file expired at %s; start a new enrollment in the console", h.File.ExpiresAt.Format(time.RFC3339))
	}
	server, caSHA256, err := targetTrust(h.Kube)
	if err != nil {
		return err
	}
	admin, err := kube.NewClient(h.Kube)
	if err != nil {
		return err
	}
	identity, err := kube.WhoAmI(ctx, admin)
	if err != nil {
		return fmt.Errorf("enrollment: ask the target who you are: %w", err)
	}
	if missing := h.missingRights(ctx, admin); len(missing) > 0 {
		return fmt.Errorf("enrollment: %s may not do what installing the agent takes: %s", identity.Username, strings.Join(missing, "; "))
	}
	summary := h.summary(server, caSHA256, identity.Username)
	if !h.operatorPresent(ctx, admin) {
		summary += "\n  The operator's API is not served here. Install Ptah Operator separately; the agent shows nothing without it.\n"
	}
	if h.Confirm != nil {
		approved, err := h.Confirm(summary)
		if err != nil {
			return err
		}
		if !approved {
			return ErrDeclined
		}
	} else {
		fmt.Fprint(h.Out, summary)
	}
	credential, err := h.credential(ctx, admin)
	if err != nil {
		return err
	}
	if err := h.complete(ctx, server, caSHA256, credential); err != nil {
		return err
	}
	if err := h.writeValues(); err != nil {
		return err
	}
	fmt.Fprintf(h.Out, "\nThe console took the enrollment. Install the agent with:\n\n"+
		"  helm upgrade --install %s %s --version %s \\\n    --namespace %s --values %s\n\n"+
		"Then activate the connection in the console once it shows the agent connected.\n",
		h.Release, h.File.Chart.Reference, h.File.Chart.Version, h.Namespace, h.ValuesPath)
	return nil
}

// targetTrust is the target's API server and the CA the context trusts for
// it. A context that does not verify the server cannot enroll it.
func targetTrust(config *rest.Config) (server, caSHA256 string, err error) {
	if config.Insecure {
		return "", "", errors.New("enrollment: the context does not verify the API server's certificate; an enrollment records the CA it trusts")
	}
	ca := config.CAData
	if len(ca) == 0 && config.CAFile != "" {
		if ca, err = os.ReadFile(config.CAFile); err != nil { // #nosec G304 -- the CA file the administrator's kubeconfig names.
			return "", "", fmt.Errorf("enrollment: read the context's CA: %w", err)
		}
	}
	if len(ca) == 0 {
		return "", "", errors.New("enrollment: the context names no CA for the API server; an enrollment records the CA it trusts")
	}
	if !strings.HasPrefix(config.Host, "https://") {
		return "", "", fmt.Errorf("enrollment: the context's server %q is not https", config.Host)
	}
	return strings.TrimSuffix(config.Host, "/"), CASHA256(string(ca)), nil
}

// missingRights is what the administrator may not do that installing the
// agent takes, as the target's API server answers.
func (h *Helper) missingRights(ctx context.Context, admin client.Client) []string {
	checks := []authorizationv1.ResourceAttributes{
		{Verb: "create", Resource: "namespaces"},
		{Verb: "create", Resource: "secrets", Namespace: h.Namespace},
		{Verb: "get", Resource: "secrets", Namespace: h.Namespace},
		{Verb: "create", Group: "rbac.authorization.k8s.io", Resource: "clusterroles"},
		{Verb: "create", Group: "rbac.authorization.k8s.io", Resource: "clusterrolebindings"},
		{Verb: "impersonate", Resource: "users"},
	}
	for _, namespace := range h.File.Identity.Namespaces {
		checks = append(checks,
			authorizationv1.ResourceAttributes{Verb: "create", Group: "rbac.authorization.k8s.io", Resource: "roles", Namespace: namespace},
			authorizationv1.ResourceAttributes{Verb: "create", Group: "rbac.authorization.k8s.io", Resource: "rolebindings", Namespace: namespace})
	}
	var missing []string
	for _, check := range checks {
		review := &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &check}}
		if err := admin.Create(ctx, review); err != nil || !review.Status.Allowed {
			where := ""
			if check.Namespace != "" {
				where = " in " + check.Namespace
			}
			missing = append(missing, check.Verb+" "+check.Resource+where)
		}
	}
	return missing
}

// operatorPresent says whether the target serves the operator's API.
func (h *Helper) operatorPresent(ctx context.Context, admin client.Client) bool {
	_, err := admin.RESTMapper().RESTMapping(schemaKind.GroupKind(), schemaKind.Version)
	return err == nil && ctx.Err() == nil
}

// summary is what the administrator approves.
func (h *Helper) summary(server, caSHA256, username string) string {
	identity := h.File.Identity
	var b strings.Builder
	fmt.Fprintf(&b, "Enroll this cluster in the Ptah Console at %s\n\n", h.File.Console.Address)
	fmt.Fprintf(&b, "  Fingerprint     %s\n", h.File.Fingerprint)
	fmt.Fprintf(&b, "                  compare it with the one the console showed\n")
	fmt.Fprintf(&b, "  Cluster         %s, connection %s\n", identity.ClusterName, identity.ConnectionUID)
	fmt.Fprintf(&b, "  Context         %s, as %s\n", h.ContextName, username)
	fmt.Fprintf(&b, "  API server      %s, CA %s\n", server, caSHA256)
	fmt.Fprintf(&b, "  Console         %s, CA %s\n", identity.ConsoleServerName, identity.ConsoleCASHA256)
	fmt.Fprintf(&b, "  Agent           release %s in namespace %s, chart %s %s\n", h.Release, h.Namespace, h.File.Chart.Reference, h.File.Chart.Version)
	fmt.Fprintf(&b, "  Namespaces      %s\n", strings.Join(identity.Namespaces, ", "))
	fmt.Fprintf(&b, "  Accounts        each acts as ptah-console:%s:<id>, which the agent may impersonate\n", identity.InstallationID)
	for _, account := range slices.SortedFunc(slices.Values(identity.Accounts), func(a, b Account) int { return strings.Compare(a.ID, b.ID) }) {
		fmt.Fprintf(&b, "                  %s: %s in every namespace above\n", account.ID, strings.Join(account.Roles, ", "))
	}
	fmt.Fprintf(&b, "  Approver role   %s\n", identity.ApproverClusterRole)
	return b.String()
}

// credential is the channel credential in the agent's Secret: the one a
// previous run of this enrollment wrote, or a new one.
func (h *Helper) credential(ctx context.Context, admin client.Client) (string, error) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: h.Namespace}}
	if err := admin.Create(ctx, namespace); err != nil && !apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("enrollment: create namespace %s: %w", h.Namespace, err)
	}
	key := client.ObjectKey{Namespace: h.Namespace, Name: h.Release + "-credential"}
	existing := &corev1.Secret{}
	err := admin.Get(ctx, key, existing)
	switch {
	case err == nil:
		if existing.Annotations[connectionAnnotation] != h.File.Identity.ConnectionUID || len(existing.Data[credentialKey]) == 0 {
			return "", fmt.Errorf("enrollment: Secret %s holds another connection's credential; remove it or choose another release name", key)
		}
		return string(existing.Data[credentialKey]), nil
	case !apierrors.IsNotFound(err):
		return "", fmt.Errorf("enrollment: read Secret %s: %w", key, err)
	}
	credential, err := newSecret()
	if err != nil {
		return "", err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace, Name: key.Name,
			Labels:      map[string]string{"app.kubernetes.io/name": "ptah-console-agent", "app.kubernetes.io/instance": h.Release},
			Annotations: map[string]string{connectionAnnotation: h.File.Identity.ConnectionUID},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{credentialKey: []byte(credential)},
	}
	if err := admin.Create(ctx, secret); err != nil {
		return "", fmt.Errorf("enrollment: create Secret %s: %w", key, err)
	}
	return credential, nil
}

// complete reports the enrollment to the console's agent listener.
func (h *Helper) complete(ctx context.Context, server, caSHA256, credential string) error {
	transport := h.Transport
	if transport == nil {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(h.File.Console.CABundle)) {
			return errors.New("enrollment: the file's console CA holds no certificate")
		}
		pinned := &http.Transport{
			TLSClientConfig:   &tls.Config{RootCAs: roots, ServerName: h.File.Console.ServerName, MinVersion: tls.VersionTLS13},
			ForceAttemptHTTP2: true,
		}
		pinned.Protocols = new(http.Protocols)
		pinned.Protocols.SetHTTP2(true)
		defer pinned.CloseIdleConnections()
		transport = pinned
	}
	console := agentv1connect.NewEnrollmentServiceClient(connect.NewClient(connecthttp.NewTransport(
		&http.Client{Transport: transport}, h.File.Console.Address)))
	_, err := console.CompleteEnrollment(ctx, &agentv1.CompleteEnrollmentRequest{
		ConnectionUid: h.File.Identity.ConnectionUID, Bootstrap: h.File.Bootstrap, Fingerprint: h.File.Fingerprint,
		TargetServer: server, TargetCaSha256: caSHA256, CredentialVerifier: Verifier(credential),
	})
	if err != nil {
		return fmt.Errorf("enrollment: the console did not take the enrollment: %w", err)
	}
	return nil
}

// values is the agent chart's values for the enrollment.
func (h *Helper) values() map[string]any {
	identity := h.File.Identity
	accounts := make([]map[string]any, 0, len(identity.Accounts))
	for _, account := range identity.Accounts {
		accounts = append(accounts, map[string]any{"id": account.ID, "roles": account.Roles})
	}
	return map[string]any{
		"installationID": identity.InstallationID,
		"connection":     map[string]any{"uid": identity.ConnectionUID, "generation": identity.Generation},
		"console": map[string]any{
			"address": h.File.Console.Address, "serverName": h.File.Console.ServerName, "caBundle": h.File.Console.CABundle,
		},
		"credential":          map[string]any{"secretName": h.Release + "-credential", "key": credentialKey},
		"namespaces":          identity.Namespaces,
		"accounts":            accounts,
		"approverClusterRole": identity.ApproverClusterRole,
	}
}

func (h *Helper) writeValues() error {
	encoded, err := yaml.Marshal(h.values())
	if err != nil {
		return err
	}
	header := "# The ptah-console-agent chart's values for enrollment " + h.File.Fingerprint + ".\n" +
		"# Written by ptah-console cluster enroll; the credential is in its Secret, not here.\n"
	return os.WriteFile(h.ValuesPath, append([]byte(header), encoded...), 0o600)
}

// schemaKind is how the helper asks whether the operator is installed.
var schemaKind = operatorv1alpha1.GroupVersion.WithKind("PtahSchema")

// ErrNotThisConnection is a credential Secret enroll did not make for the
// connection named.
var ErrNotThisConnection = errors.New("enrollment: the Secret is not the credential enroll made for that connection; it was left alone")

// Unenroll removes what enroll made on the target that Helm does not own:
// the agent's credential Secret, and only when it is the named connection's.
// It removes nothing else; the Helm release goes with helm uninstall.
func Unenroll(ctx context.Context, admin client.Client, namespace, release, connectionUID string) error {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: namespace, Name: release + "-credential"}
	if err := admin.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("enrollment: read Secret %s: %w", key, err)
	}
	if secret.Annotations[connectionAnnotation] != connectionUID {
		return fmt.Errorf("%w: %s", ErrNotThisConnection, key)
	}
	if err := admin.Delete(ctx, secret, client.Preconditions{UID: &secret.UID}); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("enrollment: remove Secret %s: %w", key, err)
	}
	return nil
}
