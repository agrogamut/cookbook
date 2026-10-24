//go:build !linux

package bookdocx

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
