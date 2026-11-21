package ws

import "testing"

func TestAcceptKey(t *testing.T) {
	// example handshake from RFC 6455 section 1.3
	if got := AcceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("got %s", got)
	}
}
