package app

import (
	"os/exec"
	"runtime"
)

// systemPower shuts the computer down, sleeps it or hibernates it, as the
// Scheduler's "when done" asks. It is best effort: a system that refuses
// simply stays on.
func systemPower(action string) {
	switch runtime.GOOS {
	case "windows":
		switch action {
		case "shutdown":
			_ = exec.Command("shutdown", "/s", "/t", "30").Start()
		case "sleep":
			_ = exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Start()
		case "hibernate":
			_ = exec.Command("shutdown", "/h").Start()
		}
	case "darwin":
		switch action {
		case "shutdown":
			_ = exec.Command("osascript", "-e", `tell application "System Events" to shut down`).Start()
		case "sleep":
			_ = exec.Command("pmset", "sleepnow").Start()
		case "hibernate":
			_ = exec.Command("pmset", "sleepnow").Start()
		}
	default:
		switch action {
		case "shutdown":
			_ = exec.Command("systemctl", "poweroff").Start()
		case "sleep":
			_ = exec.Command("systemctl", "suspend").Start()
		case "hibernate":
			_ = exec.Command("systemctl", "hibernate").Start()
		}
	}
}
