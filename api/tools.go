//go:build tools

// Package api holds the OpenAPI 3.1 source of truth (api/openapi.yaml) and
// the generated bindings (openapi.gen.go). The tools.go file pins the
// oapi-codegen dependency so `go generate ./...` and CI can resolve it.
package api

import _ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
