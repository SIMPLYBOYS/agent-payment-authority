// Command fake-claude stands in for the Claude Code CLI in fake mode. It runs
// no model and makes no network request; see internal/buyer/fakecli.
package main

import (
	"os"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/buyer/fakecli"
)

func main() {
	os.Exit(fakecli.Main(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}
