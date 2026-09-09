package codexquota

import "io"

type launchedProcess struct {
	process processHandle
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
}
