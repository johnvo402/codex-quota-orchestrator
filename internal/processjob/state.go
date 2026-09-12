package processjob

// State describes whether the current process is attached to a Windows Job
// Object. Supported is false on non-Windows platforms.
type State struct {
	Supported bool
	InJob     bool
}
