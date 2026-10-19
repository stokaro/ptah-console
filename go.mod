module github.com/stokaro/ptah-console

go 1.26.0

toolchain go1.27.2

require (
	connectrpc.com/connect/v2 v2.0.1
	github.com/frankban/quicktest v1.14.6
	github.com/spf13/cobra v1.10.2
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/go-extras/nolintguard v0.4.2 // indirect
	github.com/go-extras/qtlint v1.15.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/stokaro/ptah-console/web v0.0.0-00010101000000-000000000000
	github.com/stokaro/teststyle v0.2.0 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
)

tool (
	connectrpc.com/connect/v2/cmd/protoc-gen-connect-go
	github.com/go-extras/nolintguard/cmd/nolintguard
	github.com/go-extras/qtlint/cmd/qtlint
	github.com/stokaro/teststyle/cmd/teststyle
	golang.org/x/vuln/cmd/govulncheck
	google.golang.org/protobuf/cmd/protoc-gen-go
)

replace github.com/stokaro/ptah-console/web => ./web
