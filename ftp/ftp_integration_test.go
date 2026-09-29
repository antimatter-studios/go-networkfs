//go:build ftp_integration

// Integration tests for the FTP driver against the containerised vsftpd the
// test rig runs, rather than the embedded Go server ftp_test.go starts.
//
// They exist for one property the embedded server cannot have: that the
// server a developer brings up with `chore servers:up` is STILL THERE for the
// second client. It was not (issue #27). vsftpd's standalone listener assumes
// every child it reaps is a session it forked, and dereferences NULL when it
// is not. Running as the container's PID 1 it inherits children it never
// forked — every orphaned session process, and the image entrypoint's
// backgrounded log tails — so the container exited 139 after a first client,
// and the second one found nothing listening.
//
//	chore test               (every tagged suite, in the runner container)
//	chore test:integration   (the same on this host's toolchain)
//
// Without the tag the file is not built, so a plain `go test ./...` never
// needs a server.

package ftp

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func requireEnv(t *testing.T) map[string]string {
	t.Helper()
	cfg := map[string]string{
		"host": os.Getenv("FTP_HOST"),
		"port": os.Getenv("FTP_PORT"),
		"user": os.Getenv("FTP_USER"),
		"pass": os.Getenv("FTP_PASS"),
	}
	var missing []string
	for _, v := range []string{"FTP_HOST", "FTP_PORT", "FTP_USER", "FTP_PASS"} {
		if os.Getenv(v) == "" {
			missing = append(missing, v)
		}
	}
	// A failure, not a skip: the ftp_integration tag says there is a server.
	if len(missing) > 0 {
		t.Fatalf("FTP integration cannot run: %s unset. Run `chore test` (or "+
			"`chore test:integration`), which starts the server and exports them.",
			strings.Join(missing, ", "))
	}
	return cfg
}

// session is one whole client lifetime through the driver: mount, write a
// file, read it back, unmount.
func session(t *testing.T, cfg map[string]string, n int) {
	t.Helper()
	d := &FTPDriver{}
	if err := d.Mount(1, cfg); err != nil {
		t.Fatalf("session %d: mount: %v", n, err)
	}
	defer func() { _ = d.Unmount(1) }()

	path := "/gonfs-sessions-" + time.Now().Format("150405.000000") + ".txt"
	body := []byte("session body\n")
	w, err := d.CreateFile(1, path)
	if err != nil {
		t.Fatalf("session %d: CreateFile: %v", n, err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatalf("session %d: write: %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("session %d: close: %v", n, err)
	}
	r, err := d.OpenFile(1, path)
	if err != nil {
		t.Fatalf("session %d: OpenFile: %v", n, err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("session %d: read: %v", n, err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("session %d: read %q, wrote %q", n, got, body)
	}
	if err := d.Remove(1, path); err != nil {
		t.Fatalf("session %d: Remove: %v", n, err)
	}
}

// TestIntegrationSecondSessionFindsTheServer is the report in #27 as a test:
// one client session, then another. The second used to be refused.
func TestIntegrationSecondSessionFindsTheServer(t *testing.T) {
	cfg := requireEnv(t)
	session(t, cfg, 1)
	session(t, cfg, 2)
}

// abruptLogin logs in on a raw control connection and drops it without QUIT,
// the way a client that crashes or loses its network does. It is also the
// shape that leaves vsftpd's per-session processes to exit in either order,
// which is how an orphan reaches the listener.
func abruptLogin(cfg map[string]string) error {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(cfg["host"], cfg["port"]), 5*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	expect := func(code string) error {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return err
			}
			// Multi-line replies continue with "NNN-"; the last is "NNN ".
			if strings.HasPrefix(line, code+" ") {
				return nil
			}
			if len(line) >= 4 && line[3] == ' ' {
				return &unexpectedReply{want: code, got: strings.TrimSpace(line)}
			}
		}
	}
	if err := expect("220"); err != nil {
		return err
	}
	if _, err := io.WriteString(c, "USER "+cfg["user"]+"\r\n"); err != nil {
		return err
	}
	if err := expect("331"); err != nil {
		return err
	}
	if _, err := io.WriteString(c, "PASS "+cfg["pass"]+"\r\n"); err != nil {
		return err
	}
	return expect("230")
}

type unexpectedReply struct{ want, got string }

func (e *unexpectedReply) Error() string { return "want " + e.want + ", got " + e.got }

// TestIntegrationServerOutlivesAbruptClients drops two hundred logged-in
// control connections without a QUIT, back to back, and then runs a whole
// session. Before the fix the container died inside the loop: at 100 rounds,
// measured on an arm64 host, it died in five runs of six (at the 1st, 67th,
// 11th, 32nd and 29th client) and survived one — so a single pair of sessions
// is not enough to see it on every machine, and the count is doubled from
// there to make a pass mean something.
func TestIntegrationServerOutlivesAbruptClients(t *testing.T) {
	cfg := requireEnv(t)
	const rounds = 200
	for i := 1; i <= rounds; i++ {
		if err := abruptLogin(cfg); err != nil {
			t.Fatalf("abrupt client %d of %d: %v — the server did not survive the ones before it",
				i, rounds, err)
		}
	}
	session(t, cfg, rounds+1)
}
