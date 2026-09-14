# Add a custom component (anything with Start and Stop)

```go path=main.go
package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/guilhermebr/gox"
)

// tcpEcho owns a listener: bound in Start (so a busy port fails the boot),
// served in Run, closed in Stop.
type tcpEcho struct {
	addr string
	ln   net.Listener
}

func (e *tcpEcho) Name() string { return "tcp-echo" }

func (e *tcpEcho) Start(context.Context) error {
	ln, err := net.Listen("tcp", e.addr)
	if err != nil {
		return fmt.Errorf("tcp-echo: listen %s: %w", e.addr, err)
	}
	e.ln = ln
	return nil
}

func (e *tcpEcho) Run(ctx context.Context) error {
	for {
		conn, err := e.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // Stop closed the listener
			}
			return err // fatal: the app shuts down
		}
		go func() {
			defer conn.Close()
			buf := make([]byte, 1024)
			n, _ := conn.Read(buf)
			_, _ = conn.Write(buf[:n])
		}()
	}
}

func (e *tcpEcho) Stop(context.Context) error { return e.ln.Close() }

func main() {
	a := gox.MustNew("echo",
		gox.Component(&tcpEcho{addr: ":7"}), // registered at StageUser
	)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
