package instance

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testAddress keeps socket paths short. The sun_path limit is 104 bytes on
// macOS and 108 on Linux, and t.TempDir() alone can approach it.
func testAddress(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	address := filepath.Join(dir, "s")
	if len(address) > 100 {
		t.Skipf("temporary path %q is too long for a unix socket", address)
	}
	return address
}

// serveOnce answers deliveries in the background the way the viewer does.
func serveOnce(t *testing.T, server *Server, handle func(Request) error) {
	t.Helper()
	go func() {
		for delivery := range server.Deliveries() {
			delivery.Reply(handle(delivery.Request))
		}
	}()
}

func TestSendDeliversAndReplies(t *testing.T) {
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	var got Request
	serveOnce(t, server, func(r Request) error { got = r; return nil })

	want := Request{Command: "open", Path: "/books/paper.pdf", Page: 7, X: 100, Y: 200, HasPoint: true}
	if _, err := Send(address, want); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got != want {
		t.Fatalf("delivered %+v, want %+v", got, want)
	}
}

func TestSendReportsHandlerError(t *testing.T) {
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serveOnce(t, server, func(Request) error { return errors.New("no such page") })

	response, err := Send(address, Request{Command: "open", Path: "/x.pdf"})
	if err == nil {
		t.Fatal("expected the handler error to reach the client")
	}
	if response.OK || response.Error != "no such page" {
		t.Fatalf("response = %+v", response)
	}
}

func TestSendWithNoInstanceFails(t *testing.T) {
	address := testAddress(t)
	if _, err := Send(address, Request{Command: "ping"}); err == nil {
		t.Fatal("expected an error when nothing is listening")
	}
}

// A ping must be answered by the accept goroutine, so liveness does not depend
// on the viewer thread. A window busy rendering still counts as in use.
func TestPingDoesNotNeedTheViewer(t *testing.T) {
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	// Deliberately never drain Deliveries.
	if _, err := Send(address, Request{Command: "ping"}); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestListenRefusesALiveInstance(t *testing.T) {
	address := testAddress(t)
	first, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := Listen(address)
	if !errors.Is(err, ErrInUse) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("err = %v, want ErrInUse", err)
	}
}

// A socket left behind by a crash must be reclaimed, not treated as live.
func TestListenReclaimsAStaleSocket(t *testing.T) {
	address := testAddress(t)
	first, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	// Go unlinks the socket when a listener closes cleanly, so suppress that to
	// imitate a process killed outright, which leaves the file behind.
	first.listener.(*net.UnixListener).SetUnlinkOnClose(false)
	first.listener.Close()
	if _, err := os.Stat(address); err != nil {
		t.Fatalf("expected a leftover socket file: %v", err)
	}

	second, err := Listen(address)
	if err != nil {
		t.Fatalf("stale socket was not reclaimed: %v", err)
	}
	defer second.Close()
	serveOnce(t, second, func(Request) error { return nil })
	if _, err := Send(address, Request{Command: "open", Path: "/x.pdf"}); err != nil {
		t.Fatalf("reclaimed socket does not work: %v", err)
	}
}

// A plain file sitting at the address is also stale: nothing answers it.
func TestListenReclaimsANonSocketFile(t *testing.T) {
	address := testAddress(t)
	if err := os.WriteFile(address, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := Listen(address)
	if err != nil {
		t.Fatalf("leftover file was not reclaimed: %v", err)
	}
	defer server.Close()
	serveOnce(t, server, func(Request) error { return nil })
	if _, err := Send(address, Request{Command: "ping"}); err != nil {
		t.Fatal(err)
	}
}

func TestCloseRemovesTheSocket(t *testing.T) {
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(address); !os.IsNotExist(err) {
		t.Fatal("close left the socket behind")
	}
	// Close is idempotent, so a deferred close after an explicit one is safe.
	if err := server.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestSocketIsOwnerOnly(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Windows protects Unix sockets with ACLs, not Unix mode bits")
	}
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	info, err := os.Lstat(address)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("socket mode = %o, want no group or other access", perm)
	}
}

func TestConcurrentSendsAreAllDelivered(t *testing.T) {
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	var mu sync.Mutex
	seen := map[string]bool{}
	serveOnce(t, server, func(r Request) error {
		mu.Lock()
		defer mu.Unlock()
		seen[r.Path] = true
		return nil
	})

	const count = 24
	var wg sync.WaitGroup
	errs := make([]error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = Send(address, Request{Command: "open", Path: fmt.Sprintf("/doc-%d.pdf", i)})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("send %d: %v", i, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != count {
		t.Fatalf("delivered %d of %d requests", len(seen), count)
	}
}

// Garbage on the connection must be rejected without disturbing the server.
func TestMalformedRequestIsRejected(t *testing.T) {
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serveOnce(t, server, func(Request) error { return nil })

	conn, err := net.Dial("unix", address)
	if err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte("not json at all\n"))
	conn.Close()

	if _, err := Send(address, Request{Command: "ping"}); err != nil {
		t.Fatalf("server did not survive a malformed request: %v", err)
	}
}

// A client must not hang forever on an instance that never answers.
func TestUnansweredRequestTimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("takes replyTimeout to run")
	}
	address := testAddress(t)
	server, err := Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	// Drain deliveries but never reply, imitating a wedged viewer.
	go func() {
		for range server.Deliveries() {
		}
	}()

	start := time.Now()
	_, err = Send(address, Request{Command: "open", Path: "/x.pdf"})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > replyTimeout+5*time.Second {
		t.Fatalf("took %v to give up", elapsed)
	}
}

func TestAddressIsStableAndPrivate(t *testing.T) {
	first, err := AddressFor("/books/paper.pdf")
	if err != nil {
		t.Fatal(err)
	}
	second, err := AddressFor("/books/paper.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("address is not stable: %q then %q", first, second)
	}
	if !filepath.IsAbs(first) {
		t.Fatalf("address %q is not absolute", first)
	}
	if os.PathSeparator != '\\' {
		info, err := os.Stat(filepath.Dir(first))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("directory mode = %o, want owner-only", perm)
		}
	}
	// Unix socket paths are capped at 104 bytes on macOS and 108 on Linux.
	if len(first) > 100 {
		t.Fatalf("address %q is %d bytes, too close to the socket limit", first, len(first))
	}
}

func TestAddressIsPerDocument(t *testing.T) {
	paper, err := AddressFor("/books/paper.pdf")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := AddressFor("/books/notes.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if paper == notes {
		t.Fatal("two documents share an address, so they would share a window")
	}
	if _, err := AddressFor(""); err == nil {
		t.Error("an empty document should have no address")
	}
}

// A long document path must still yield a usable socket, which is why the name
// is a hash rather than the path.
func TestAddressStaysShortForLongDocumentPaths(t *testing.T) {
	long := "/" + strings.Repeat("very-long-directory-name/", 20) + "paper.pdf"
	address, err := AddressFor(long)
	if err != nil {
		t.Fatal(err)
	}
	if len(address) > 100 {
		t.Fatalf("address for a %d byte path is %d bytes", len(long), len(address))
	}
	// It must still be usable as a real socket.
	server, err := Listen(address)
	if err != nil {
		t.Fatalf("listen on hashed address: %v", err)
	}
	defer server.Close()
	if _, err := Send(address, Request{Command: "ping"}); err != nil {
		t.Fatal(err)
	}
}

func TestParseTarget(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		spec     string
		wantNil  bool
		wantPage int
		wantX    float64
		wantY    float64
		wantXY   bool
		wantErr  string
	}{
		{name: "empty means no jump", spec: "", wantNil: true},
		{name: "whitespace means no jump", spec: "   ", wantNil: true},
		{name: "page only", spec: "7", wantPage: 7},
		{name: "page is trimmed", spec: " 7 ", wantPage: 7},
		{name: "page and point", spec: "3:100.5:250.25", wantPage: 3, wantX: 100.5, wantY: 250.25, wantXY: true},
		{name: "negative coordinates are allowed", spec: "1:-5:-10", wantPage: 1, wantX: -5, wantY: -10, wantXY: true},
		{name: "zero page is rejected", spec: "0", wantErr: "1-based page"},
		{name: "negative page is rejected", spec: "-2", wantErr: "1-based page"},
		{name: "non-numeric page is rejected", spec: "first", wantErr: "1-based page"},
		{name: "two fields is ambiguous", spec: "3:100", wantErr: "want PAGE or PAGE:X:Y"},
		{name: "four fields is rejected", spec: "3:1:2:4", wantErr: "want PAGE or PAGE:X:Y"},
		{name: "unreadable x is rejected", spec: "3:left:250", wantErr: "unreadable coordinate"},
		{name: "unreadable y is rejected", spec: "3:100:down", wantErr: "unreadable coordinate"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ParseTarget(testCase.spec)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, testCase.wantErr)
				}
				if got != nil {
					t.Fatal("a rejected spec must not yield a request")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if testCase.wantNil {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want a request")
			}
			if got.Page != testCase.wantPage || got.X != testCase.wantX ||
				got.Y != testCase.wantY || got.HasPoint != testCase.wantXY {
				t.Fatalf("got page=%d x=%v y=%v hasPoint=%v, want page=%d x=%v y=%v hasPoint=%v",
					got.Page, got.X, got.Y, got.HasPoint,
					testCase.wantPage, testCase.wantX, testCase.wantY, testCase.wantXY)
			}
		})
	}
}

func TestLastWindowIsRecordedWhole(t *testing.T) {
	dir := filepath.Dir(testAddress(t))
	t.Setenv("TMPDIR", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("LOCALAPPDATA", dir)
	if _, ok := ReadLastWindow(); ok {
		t.Fatal("a last window before any was noted")
	}
	address, err := WindowAddressFor(42)
	if err != nil {
		t.Fatal(err)
	}
	want := LastWindow{Address: address, Args: []string{"-config=/c.lua"}}
	if err := NoteLastWindow(want); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadLastWindow()
	if !ok || got.Address != want.Address || !got.SameArgs(want.Args) || got.SameArgs(nil) {
		t.Fatalf("read %+v, want %+v", got, want)
	}
}
