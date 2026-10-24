// Package api implements the console's Connect services.
//
// A handler here reads Kubernetes only through the client bound to the
// caller's identity, and returns a *connect.Error for anything it wants the
// browser to read: connect-go v2 sends any other error as a bare code.
package api

import (
	"context"
	"encoding/json"

	"github.com/stokaro/ptah-operator/pkg/planread"

	"github.com/stokaro/ptah-console/internal/buildinfo"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
	"github.com/stokaro/ptah-console/internal/release"
	"github.com/stokaro/ptah-console/support"
)

// Info serves InfoService.
type Info struct {
	consolev1connect.UnimplementedInfoServiceHandler

	mode     consolev1.Mode
	readOnly bool
	facts    InfoFacts
}

// InfoFacts say how people sign in to the console.
type InfoFacts struct {
	SignIn consolev1.SignIn
	// SignInProvider names the OpenID Connect provider on the sign-in page.
	SignInProvider string
}

// NewInfo returns the InfoService for a console running in mode.
func NewInfo(mode consolev1.Mode, readOnly bool, facts InfoFacts) *Info {
	return &Info{mode: mode, readOnly: readOnly, facts: facts}
}

// GetInfo returns the build and the mode.
func (i *Info) GetInfo(context.Context, *consolev1.GetInfoRequest) (*consolev1.GetInfoResponse, error) {
	build := buildinfo.Read()
	return &consolev1.GetInfoResponse{
		Version:              build.Version,
		Commit:               build.Commit,
		Mode:                 i.mode,
		ReadOnly:             i.readOnly,
		OperatorVersion:      build.Operator,
		PlanContractVersion:  readContracts[len(readContracts)-1],
		KubernetesMinors:     testedMinors(support.Compatibility),
		PlanContractVersions: readContracts,
		SignIn:               i.facts.SignIn,
		SignInProvider:       i.facts.SignInProvider,
	}, nil
}

// maxContractProbed bounds the contract versions asked of the plan reader.
const maxContractProbed = 32

// readContracts are the plan contracts the plan reader this console is built
// with reads, oldest first, asked of the reader itself rather than listed
// here, so a new operator module brings its own.
var readContracts = func() []int32 {
	var read []int32
	for version := int32(1); version <= maxContractProbed; version++ {
		if planread.SupportedContract(version) {
			read = append(read, version)
		}
	}
	if len(read) == 0 {
		panic("api: the plan reader reads no plan contract up to 32")
	}
	return read
}()

// testedMinors are the Kubernetes minors of the support record's first row,
// the one that describes this tree, or none when the record cannot be read.
// CI holds the record to the tree, so none is a build nobody checked.
func testedMinors(record []byte) []string {
	var parsed release.Record
	if err := json.Unmarshal(record, &parsed); err != nil || len(parsed.Releases) == 0 {
		return nil
	}
	return parsed.Releases[0].Kubernetes
}
