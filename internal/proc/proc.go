// Package proc prepares the commands MyInfra starts for the way it runs.
package proc

import "os/exec"

// Hide keeps cmd from opening a console window, and returns it. Only the
// Windows app, which has no console of its own, would show one per command.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	hide(cmd)
	return cmd
}
