package kube

import (
	"bytes"
	"errors"
	"flag"
	"sync"
	"testing"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

func captureKlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	flags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(flags)
	if err := flags.Set("logtostderr", "false"); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("alsologtostderr", "false"); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("stderrthreshold", "FATAL"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	klog.SetOutput(&buf)
	t.Cleanup(func() {
		klog.ClearLogger()
		klog.SetOutput(nil)
		_ = flags.Set("logtostderr", "true")
		quietOnce = sync.Once{}
	})
	klog.ClearLogger()
	quietOnce = sync.Once{}
	return &buf
}

func TestQuietClientGoDiscardsUnhandledErrors(t *testing.T) {
	buf := captureKlog(t)
	utilruntime.HandleError(errors.New("visible before quieting"))
	klog.Flush()
	if !bytes.Contains(buf.Bytes(), []byte("visible before quieting")) {
		t.Fatalf("test setup does not capture klog output: %q", buf.String())
	}

	buf.Reset()
	quietClientGo()
	utilruntime.HandleError(errors.New("An error occurred forwarding"))
	klog.Flush()
	if buf.Len() != 0 {
		t.Fatalf("client-go output was not discarded: %q", buf.String())
	}
}

func TestQuietClientGoKeepsLogsWhenDebugging(t *testing.T) {
	buf := captureKlog(t)
	t.Setenv(DebugEnv, "1")
	quietClientGo()
	utilruntime.HandleError(errors.New("kept for debugging"))
	klog.Flush()
	if !bytes.Contains(buf.Bytes(), []byte("kept for debugging")) {
		t.Fatalf("client-go output should be kept with %s set: %q", DebugEnv, buf.String())
	}
}
