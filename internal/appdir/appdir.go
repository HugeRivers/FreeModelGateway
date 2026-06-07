package appdir

import (
	"os"
	"path/filepath"
)

func Home() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic("cannot determine user home directory: " + err.Error())
	}
	return filepath.Join(home, ".fmg")
}

func LogDir() string  { return filepath.Join(Home(), "logs") }
func DBFile() string  { return filepath.Join(Home(), "data.db") }
func PidFile() string { return filepath.Join(Home(), "fmg.pid") }

func EnsureAll() error {
	dirs := []string{Home(), LogDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0750); err != nil {
			return err
		}
	}
	return nil
}
