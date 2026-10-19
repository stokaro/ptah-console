package local

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens url with the platform's opener. The URL is an argument,
// never part of a shell command line.
func OpenBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open the browser: %w", err)
	}
	go func() { _ = command.Wait() }()
	return nil
}
