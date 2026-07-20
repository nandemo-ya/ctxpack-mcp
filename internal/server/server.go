// Package server builds the MCP server that exposes the ctxpack CLI as tools.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nandemo-ya/ctxpack-mcp/internal/ctxpack"
)

// New returns an MCP server with every ctxpack tool registered.
//
// A missing or outdated ctxpack does not stop the server from starting: tools
// are registered either way and report the problem when called, so the client
// can show the user how to fix it instead of failing to connect.
func New(version string) *mcp.Server {
	return newWithRunner(version, &ctxpack.Runner{})
}

func newWithRunner(version string, runner *ctxpack.Runner) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:       "ctxpack-mcp",
		Title:      "ctxpack",
		Version:    version,
		WebsiteURL: "https://github.com/nandemo-ya/ctxpack-mcp",
	}, nil)
	addTools(s, runner)
	return s
}
