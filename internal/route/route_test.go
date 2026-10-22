package route_test

import (
	"slices"
	"testing"

	qt "github.com/frankban/quicktest"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	consolev1 "github.com/stokaro/ptah-console/internal/gen/ptah/console/v1"
	"github.com/stokaro/ptah-console/internal/route"
)

// consoleOwn are the services that are the console's own, whichever cluster
// a page shows: the session and the console's information.
var consoleOwn = []string{"ptah.console.v1.SessionService", "ptah.console.v1.InfoService"}

// procedures are every procedure of the console's API, by whether it is the
// console's own.
func procedures() (own, cluster []string) {
	protoregistry.GlobalFiles.RangeFilesByPackage("ptah.console.v1", func(file protoreflect.FileDescriptor) bool {
		services := file.Services()
		for index := range services.Len() {
			service := services.Get(index)
			methods := service.Methods()
			for m := range methods.Len() {
				procedure := "/" + string(service.FullName()) + "/" + string(methods.Get(m).Name())
				if slices.Contains(consoleOwn, string(service.FullName())) {
					own = append(own, procedure)
				} else {
					cluster = append(cluster, procedure)
				}
			}
		}
		return true
	})
	return own, cluster
}

// Every procedure of the console's API follows the connection a request
// names, except the console's own, so a new procedure cannot quietly be
// served from the wrong cluster.
func TestEveryClusterProcedureFollowsTheConnection(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	_ = consolev1.File_ptah_console_v1_resources_proto

	own, cluster := procedures()

	c.Assert(cluster, qt.HasLen, 15)
	for _, procedure := range cluster {
		c.Assert(route.Routed(procedure), qt.IsTrue, qt.Commentf("%s", procedure))
	}
	c.Assert(own, qt.HasLen, 5)
	for _, procedure := range own {
		c.Assert(route.Routed(procedure), qt.IsFalse, qt.Commentf("%s", procedure))
	}
}
