//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/free-model-gateway/fmg/internal/appdir"
	"github.com/getlantern/systray"
)

var (
	fmgCmd   *exec.Cmd
	fmgMutex sync.Mutex
	running  bool
)

//go:embed assets/tray-running.ico
var iconRunning []byte

//go:embed assets/tray-stopped.ico
var iconStopped []byte

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle("FMG")
	systray.SetTooltip("Free Model Gateway")
	systray.SetIcon(iconStopped)

	mDashboard := systray.AddMenuItem("Open Dashboard", "Open dashboard in browser")
	systray.AddSeparator()
	mStart := systray.AddMenuItem("Start Service", "Start FMG service")
	mStop := systray.AddMenuItem("Stop Service", "Stop FMG service")
	systray.AddSeparator()
	mRestart := systray.AddMenuItem("Restart Service", "Restart FMG service")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit FMG")

	go func() {
		for range mDashboard.ClickedCh {
			openDashboard()
		}
	}()
	go func() {
		for range mStart.ClickedCh {
			startService()
		}
	}()
	go func() {
		for range mStop.ClickedCh {
			stopService()
		}
	}()
	go func() {
		for range mRestart.ClickedCh {
			restartService()
		}
	}()
	go func() {
		for range mQuit.ClickedCh {
			systray.Quit()
		}
	}()

	initHome()
	startService()
}

func onExit() {
	stopService()
}

func initHome() {
	_ = appdir.EnsureAll()
}

func startService() {
	fmgMutex.Lock()
	defer fmgMutex.Unlock()

	if running {
		return
	}

	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	fmgBin := filepath.Join(exeDir, "fmg.exe")

	// Check if fmg.exe exists in same directory
	if _, err := os.Stat(fmgBin); os.IsNotExist(err) {
		// Try current working directory
		fmgBin = "fmg.exe"
	}

	// Pass web-app path relative to executable
	webAppPath := filepath.Join(exeDir, "web-app")

	fmgCmd = exec.Command(fmgBin, "-web-app", webAppPath)
	fmgCmd.Stdout = os.Stdout
	fmgCmd.Stderr = os.Stderr

	if err := fmgCmd.Start(); err != nil {
		return
	}
	running = true
	systray.SetIcon(iconRunning)

	go func() {
		_ = fmgCmd.Wait()
		fmgMutex.Lock()
		running = false
		systray.SetIcon(iconStopped)
		fmgMutex.Unlock()
	}()
}

func stopService() {
	fmgMutex.Lock()
	defer fmgMutex.Unlock()

	if !running || fmgCmd == nil || fmgCmd.Process == nil {
		return
	}

	_ = fmgCmd.Process.Kill()

	done := make(chan struct{})
	go func() {
		_ = fmgCmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = fmgCmd.Process.Kill()
	}

	running = false
	systray.SetIcon(iconStopped)
}

func restartService() {
	stopService()
	time.Sleep(500 * time.Millisecond)
	startService()
}

func openDashboard() {
	cmd := exec.Command("cmd", "/c", "start", "http://localhost:10086")
	_ = cmd.Start()
}

func init() {
	_ = fmt.Sprintf
	_ = runtime.GOOS
}
