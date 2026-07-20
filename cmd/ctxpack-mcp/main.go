// Command ctxpack-mcp serves the ctxpack CLI as MCP tools over stdio.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nandemo-ya/ctxpack-mcp/internal/server"
)

// version is set by release builds via -ldflags "-X main.version=...".
var version = ""

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(resolveVersion())
		return
	}

	// The MCP session owns stdout, so diagnostics go to stderr.
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A closed stdin and a signal are both ordinary shutdowns: MCP clients end a
	// session by closing the pipe, so neither should look like a crash.
	if err := server.New(resolveVersion()).Run(ctx, &mcp.StdioTransport{}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return
		}
		log.Fatalf("ctxpack-mcp: %v", err)
	}
}

// resolveVersion prefers the ldflags-injected version, then the version stamped
// into the binary by `go install`, so both release and source installs report
// something meaningful.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
