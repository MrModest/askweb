// Command askweb serves the web_fetch MCP tool over Streamable HTTP.
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MrModest/askweb/internal/config"
	"github.com/MrModest/askweb/internal/server"
	"github.com/MrModest/askweb/internal/whitelist"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("askweb: ")

	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
}

func run(args []string) error {
	cfg, err := config.Resolve(args, os.Getenv, os.Stderr)
	if err != nil {
		return err
	}

	store, err := whitelist.Open(cfg.WhitelistPath)
	if err != nil {
		return err
	}

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server.New(store, http.DefaultClient) },
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	log.Printf("serving MCP at %s/mcp, whitelist %s", cfg.Addr, cfg.WhitelistPath)
	return http.ListenAndServe(cfg.Addr, mux)
}
