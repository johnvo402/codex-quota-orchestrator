//go:build !windows

package processjob

func Current() (State, error) {
	return State{Supported: false}, nil
}
