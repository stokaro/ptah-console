// Package api implements the console's Connect services.
//
// A handler here reads Kubernetes only through the client bound to the
// caller's identity, and returns a *connect.Error for anything it wants the
// browser to read: connect-go v2 sends any other error as a bare code.
package api

import (
	"context"

	"github.com/stokaro/ptah-console/internal/buildinfo"
	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/gen/ptah/console/v1/consolev1connect"
)

// Info serves InfoService.
type Info struct {
	consolev1connect.UnimplementedInfoServiceHandler

	mode     consolev1.Mode
	readOnly bool
}

// NewInfo returns the InfoService for a console running in mode.
func NewInfo(mode consolev1.Mode, readOnly bool) *Info {
	return &Info{mode: mode, readOnly: readOnly}
}

// GetInfo returns the build and the mode.
func (i *Info) GetInfo(context.Context, *consolev1.GetInfoRequest) (*consolev1.GetInfoResponse, error) {
	build := buildinfo.Read()
	return &consolev1.GetInfoResponse{
		Version:         build.Version,
		Commit:          build.Commit,
		Mode:            i.mode,
		ReadOnly:        i.readOnly,
		OperatorVersion: build.Operator,
	}, nil
}
