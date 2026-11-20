// Package version menyimpan versi wdev. Nilai dapat ditimpa saat build:
//
//	go build -ldflags "-X wdev/internal/version.Version=2.1.0" ./cmd/wdev
package version

// Version adalah versi rilis wdev saat ini.
var Version = "2.2.0"
