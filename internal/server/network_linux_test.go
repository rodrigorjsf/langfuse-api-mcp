package server_test

import (
	"net"
	"net/url"
	"testing"
)

// TestARefusedHostKeepsItsPortForTheWholeTest proves a parallel test can never
// receive the address refusedURL handed out (#77). Linux only: macOS
// (SO_REUSEADDR) and Windows accept an explicit bind to a port held by a
// connected socket, while their port-0 allocation still skips it.
func TestARefusedHostKeepsItsPortForTheWholeTest(t *testing.T) {
	t.Parallel()
	u, err := url.Parse(refusedURL(t))
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", u.Host)

	if err == nil {
		_ = ln.Close()
		t.Fatalf("listening on %s succeeded: the helper freed a port another test can receive", u.Host)
	}
}
