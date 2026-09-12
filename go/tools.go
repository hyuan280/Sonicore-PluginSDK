//go:build tools

// Package tools pins the code-generation toolchain versions used by
// proto/regen.sh so local runs and CI produce identical output.
package tools

import (
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
