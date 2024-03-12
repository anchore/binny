package command

import (
	"os"
	"os/exec"
)

func run(path string, args []string) error {
	c := exec.Command(path, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
