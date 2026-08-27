module github.com/Silo-Server/silo-plugin-tvdb

go 1.26.3

require (
	github.com/Silo-Server/silo-plugin-sdk v0.13.2
	golang.org/x/sync v0.20.0
	golang.org/x/time v0.14.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/fatih/color v1.13.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.7.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.17 // indirect
	github.com/oklog/run v1.1.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/grpc v1.82.1 // indirect
)

// TEMPORARY: pins the SeasonNumber SDK contract from Silo-Server/silo-plugin-sdk#16.
// Must be replaced by an official tagged SDK release before this plugin PR merges.
replace github.com/Silo-Server/silo-plugin-sdk => github.com/blurbery/silo-plugin-sdk v0.13.3-0.20260821093713-0950ea324999
