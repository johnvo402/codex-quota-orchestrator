//go:build !windows

package processjob

func CurrentDesktopHost() (DesktopHost, error) {
	return DesktopHost{Supported: false}, nil
}

func ProcessAlive(pid int) (bool, error) {
	return false, nil
}
