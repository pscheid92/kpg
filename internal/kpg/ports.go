package kpg

import (
	"net"
	"strconv"
)

func FreeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = ln.Close()
	}()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func EnsurePortFree(port int) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return ln.Close()
}
