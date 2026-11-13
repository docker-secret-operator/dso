// Command composedrv is a tiny driver used ONLY by scripts/integration/run-all.sh.
// It feeds a compose document read from its own stdin to the product's real
// core.RunComposeUpStdin (so the real Docker CLI/daemon and the real wrapper are
// exercised, with no fake docker binary). SIGTERM/SIGINT cancel the context, which
// is exactly the cancellation path the product uses.
//
// It never prints the compose document. Docker is selected by the caller through
// the environment (DOCKER_HOST); the runner verified that endpoint beforehand.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/docker-secret-operator/dso/internal/core"
)

type multi []string

func (m *multi) String() string     { return fmt.Sprint(*m) }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	project := flag.String("project", "", "compose project name")
	dir := flag.String("dir", "", "absolute project directory")
	var args multi
	flag.Var(&args, "arg", "argument forwarded after `up` (repeatable)")
	flag.Parse()

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "composedrv: reading stdin:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	err = core.RunComposeUpStdin(ctx, data, core.ComposeStdinOptions{
		ProjectName: *project,
		ProjectDir:  *dir,
		Args:        args,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	})
	if err == nil {
		return
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		fmt.Fprintf(os.Stderr, "composedrv: compose exited with code %d\n", ee.ExitCode())
		if ee.ExitCode() > 0 {
			os.Exit(ee.ExitCode())
		}
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "composedrv:", err)
	os.Exit(1)
}
