package mailer

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSendMailContextCancelsStalledGreeting(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = SendMailContext(ctx, SmtpConfig{Host: "127.0.0.1", Port: uint32(ln.Addr().(*net.TCPAddr).Port), From: "ani@example.com"}, []string{"invite@example.com"}, "invite", "body")
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("SMTP connection was not closed")
	}
}
