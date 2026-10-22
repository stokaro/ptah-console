// Package registry reads the console's registry of connections as a person
// sees it.
//
// A list is not filtered by RBAC: whoever may list ConsoleClusters sees every
// name, and an ordinary account is granted single entries by exact name. So
// the console lists the names with its own identity, which may list them, and
// shows a person only the entries that person may get, asking the API server
// as that person for each one. Nothing about an entry -- its name, its status,
// or that it exists, which a count would say -- reaches a person before their
// own get of it succeeded.
package registry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolev1alpha1 "github.com/stokaro/ptah-console/api/v1alpha1"
)

const (
	// MaxEntries is the most connections one registry holds. A registry
	// past it is refused rather than shown in part.
	MaxEntries = 256
	pageSize   = 64
)

// ErrTooManyEntries is a registry holding more than MaxEntries connections.
var ErrTooManyEntries = errors.New("the registry holds more connections than the console reads")

// Visible returns the connections in namespace that user may get, sorted by
// name. housekeeping is the console's own identity and is used only to list
// the names; every entry returned is the one user's own get returned.
func Visible(ctx context.Context, housekeeping, user client.Reader, namespace string) ([]consolev1alpha1.ConsoleCluster, error) {
	names, err := names(ctx, housekeeping, namespace)
	if err != nil {
		return nil, err
	}
	visible := make([]consolev1alpha1.ConsoleCluster, 0, len(names))
	for _, name := range names {
		entry := consolev1alpha1.ConsoleCluster{}
		err := user.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &entry)
		switch {
		case err == nil:
			visible = append(visible, entry)
		case apierrors.IsForbidden(err), apierrors.IsNotFound(err):
			// Not this person's, or gone since the list: either way, nothing
			// to show and nothing to say about it.
		default:
			return nil, fmt.Errorf("read connection %s: %w", name, err)
		}
	}
	return visible, nil
}

// names lists the connection names with metadata only, page by page.
func names(ctx context.Context, reader client.Reader, namespace string) ([]string, error) {
	var names []string
	token := ""
	for {
		page := &metav1.PartialObjectMetadataList{}
		page.SetGroupVersionKind(consolev1alpha1.GroupVersion.WithKind("ConsoleClusterList"))
		options := []client.ListOption{client.InNamespace(namespace), client.Limit(pageSize)}
		if token != "" {
			options = append(options, client.Continue(token))
		}
		if err := reader.List(ctx, page, options...); err != nil {
			return nil, fmt.Errorf("list the registry: %w", err)
		}
		for _, item := range page.Items {
			names = append(names, item.Name)
		}
		if len(names) > MaxEntries {
			return nil, fmt.Errorf("%w: more than %d", ErrTooManyEntries, MaxEntries)
		}
		if token = page.Continue; token == "" {
			break
		}
	}
	slices.SortFunc(names, strings.Compare)
	return names, nil
}

// ThisCluster is what the console's configuration says about the cluster it
// runs in.
type ThisCluster struct {
	Namespace      string
	Name           string
	ConnectionUID  string
	InstallationID string
	Namespaces     []string
}

// ErrThisClusterDisagrees is a registry entry for the cluster the console runs
// in that says something else than the console's configuration.
var ErrThisClusterDisagrees = errors.New("the registry's entry for this cluster disagrees with the configuration")

// CheckThisCluster reads the entry of the cluster the console runs in with
// the console's own identity and holds it to the configuration: the co-located
// profile, the connection UID, the installation whose accounts act there, and
// the namespace ceiling. A console whose registry says another thing than its
// configuration does not start, because the two would grant different
// people different things.
func CheckThisCluster(ctx context.Context, housekeeping client.Reader, want ThisCluster) error {
	entry := consolev1alpha1.ConsoleCluster{}
	if err := housekeeping.Get(ctx, client.ObjectKey{Namespace: want.Namespace, Name: want.Name}, &entry); err != nil {
		return fmt.Errorf("read the registry's entry for this cluster %s/%s: %w", want.Namespace, want.Name, err)
	}
	spec := entry.Spec
	var problems []string
	if spec.Profile != consolev1alpha1.ProfileCoLocated {
		problems = append(problems, fmt.Sprintf("its profile is %s, not %s", spec.Profile, consolev1alpha1.ProfileCoLocated))
	}
	if spec.ConnectionUID != want.ConnectionUID {
		problems = append(problems, fmt.Sprintf("its connection UID is %s, not %s", spec.ConnectionUID, want.ConnectionUID))
	}
	if spec.InstallationID != want.InstallationID {
		problems = append(problems, fmt.Sprintf("its installation is %s, not %s", spec.InstallationID, want.InstallationID))
	}
	have, configured := slices.Clone(spec.Namespaces), slices.Clone(want.Namespaces)
	slices.Sort(have)
	slices.Sort(configured)
	if !slices.Equal(have, configured) {
		problems = append(problems, fmt.Sprintf("its namespaces are %v, not %v", have, configured))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrThisClusterDisagrees, strings.Join(problems, "; "))
	}
	return nil
}

// ErrNoSuchConnection is a connection the user may not see, which reads the
// same as one that does not exist.
var ErrNoSuchConnection = errors.New("registry: no connection with that UID is visible to you")

// Find returns the entry of the connection with connectionUID, read as the
// user. The console's own identity finds the entry's name; the user's get
// decides whether they see it.
func Find(ctx context.Context, housekeeping, user client.Reader, namespace, connectionUID string) (*consolev1alpha1.ConsoleCluster, error) {
	entries := &consolev1alpha1.ConsoleClusterList{}
	if err := housekeeping.List(ctx, entries, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list the registry: %w", err)
	}
	for _, listed := range entries.Items {
		if listed.Spec.ConnectionUID != connectionUID {
			continue
		}
		entry := &consolev1alpha1.ConsoleCluster{}
		if err := user.Get(ctx, client.ObjectKeyFromObject(&listed), entry); err != nil || entry.Spec.ConnectionUID != connectionUID {
			return nil, ErrNoSuchConnection
		}
		return entry, nil
	}
	return nil, ErrNoSuchConnection
}
