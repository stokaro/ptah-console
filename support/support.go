// Package support carries the console's support record into the binary, so
// a running console can say what its release was tested against.
package support

import _ "embed"

// Compatibility is support/compatibility.json. Its first row describes the
// tree it is in; internal/release holds it to that.
//
//go:embed compatibility.json
var Compatibility []byte
