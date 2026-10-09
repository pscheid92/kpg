package kube

import (
	"os"
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
)

// DebugEnv names the environment variable that keeps client-go's own log
// output, for example when debugging a port-forward problem.
const DebugEnv = "KPG_DEBUG"

var quietOnce sync.Once

// quietClientGo stops client-go from writing its internal log lines, such as
// "An error occurred forwarding ...", to stderr. kpg reports tunnel problems
// itself in plain language, and those lines would otherwise land in the
// middle of an interactive psql session.
func quietClientGo() {
	quietOnce.Do(func() {
		if os.Getenv(DebugEnv) != "" {
			return
		}
		klog.SetLogger(logr.Discard())
	})
}
