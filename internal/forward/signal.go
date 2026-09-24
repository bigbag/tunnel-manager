package forward

import (
	"os"
	"syscall"
)

func sigTerm() os.Signal {
	return syscall.SIGTERM
}
