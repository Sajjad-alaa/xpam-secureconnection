package webproxy

import (
	"context"
	"net"
	"testing"
	"time"
)

// A plain HTTP request is sent to the address that was checked, whatever the name resolves to by then
// (DNS rebinding), and never to an address nobody checked.
func TestDialCheckedUsesOnlyTheCheckedAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	dial := dialChecked(&net.Dialer{Timeout: 2 * time.Second})

	ctx := context.WithValue(context.Background(), checkedAddressKey{}, listener.Addr().String())
	conn, err := dial(ctx, "tcp", "rebinding.invalid:80")
	if err != nil {
		t.Fatalf("the checked address must be dialled instead of the name: %v", err)
	}
	if conn.RemoteAddr().String() != listener.Addr().String() {
		t.Fatalf("dialled %s, want the checked %s", conn.RemoteAddr(), listener.Addr())
	}
	_ = conn.Close()

	if _, err := dial(context.Background(), "tcp", listener.Addr().String()); err == nil {
		t.Fatal("a request without a checked address must not be sent")
	}
}
