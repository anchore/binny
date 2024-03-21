//go:build linux || darwin

package command

import (
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func run(path string, args []string) error {
	c := exec.Command(path, args...)

	ptmx, err := pty.Start(c)
	if err != nil {
		return err
	}

	// make sure to close the pty at the end
	defer func() { _ = ptmx.Close() }() // best effort

	// handle pty size
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
				log.Printf("error resizing pty: %s", err)
			}
		}
	}()
	ch <- syscall.SIGWINCH                        // initial resize
	defer func() { signal.Stop(ch); close(ch) }() // cleanup signals when done

	// Set stdin in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // best effort

	// copy stdin to the pty and the pty to stdout. The goroutine will keep reading until the next keystroke before returning.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	_, _ = io.Copy(os.Stdout, ptmx)

	return nil
}
