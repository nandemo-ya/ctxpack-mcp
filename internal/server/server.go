// Package server builds the MCP server that exposes the ctxpack CLI as tools.
package server

import "github.com/modelcontextprotocol/go-sdk/mcp"

// New returns an MCP server with every ctxpack tool registered.
func New(version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:       "ctxpack-mcp",
		Title:      "ctxpack",
		Version:    version,
		WebsiteURL: "https://github.com/nandemo-ya/ctxpack-mcp",
	}, nil)
	addTools(s)
	return s
}
