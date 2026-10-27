package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/pkg/sftp"
)

// SSH hosts are reached by running the system's ssh with its sftp subsystem
// and speaking SFTP over its pipes. ssh then does everything it does in a
// terminal: ~/.ssh/config, the agent, known_hosts, ProxyJump and
// ControlMaster. Its questions, such as for a password, come to the window
// through askpass (askpass.go). One connection per host stays open for later
// reads, saves and checks for changes.

// SSHCommand is the ssh run to reach a host. Tests replace it.
var SSHCommand = "ssh"

type sshConn struct {
	client *sftp.Client
	cmd    *exec.Cmd
	home   string
	stderr *tailBuffer
	done   chan struct{} // closed once ssh exits
}

func (c *sshConn) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *sshConn) close() {
	c.client.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	<-c.done
}

// hostConn holds the connection to one host. Its lock is held while
// connecting, so concurrent fetches share one ssh and one password prompt.
type hostConn struct {
	mu   sync.Mutex
	conn *sshConn
}

var (
	poolMu sync.Mutex
	pool   = map[string]*hostConn{}
)

func hostKey(t target) string {
	key := t.url.Host
	if t.url.User != nil {
		key = t.url.User.Username() + "@" + key
	}
	return key
}

func hostFor(t target) *hostConn {
	poolMu.Lock()
	defer poolMu.Unlock()
	key := hostKey(t)
	h := pool[key]
	if h == nil {
		h = &hostConn{}
		pool[key] = h
	}
	return h
}

// Close ends every SSH connection.
func Close() {
	defer closeBroker()
	poolMu.Lock()
	hosts := pool
	pool = map[string]*hostConn{}
	poolMu.Unlock()
	for _, h := range hosts {
		h.mu.Lock()
		if h.conn != nil {
			h.conn.close()
			h.conn = nil
		}
		h.mu.Unlock()
	}
}

// withSFTP runs op on a connection to t's host, connecting first if there is
// none. A connection that turns out to have dropped is replaced once.
func withSFTP(ctx context.Context, t target, op func(*sshConn) error) error {
	h := hostFor(t)
	for attempt := 0; ; attempt++ {
		h.mu.Lock()
		reused := h.conn != nil && h.conn.alive()
		if !reused {
			if h.conn != nil {
				h.conn.close()
				h.conn = nil
			}
			conn, err := dialSSH(ctx, t)
			if err != nil {
				h.mu.Unlock()
				return err
			}
			h.conn = conn
		}
		conn := h.conn
		h.mu.Unlock()
		err := op(conn)
		if reused && attempt == 0 && connectionLost(err, conn) {
			continue
		}
		return err
	}
}

// existingSFTP runs op only if t's host is already connected, for checks
// that must not start ssh, and with it perhaps a password prompt, by
// themselves.
func existingSFTP(t target, op func(*sshConn) error) (bool, error) {
	h := hostFor(t)
	if !h.mu.TryLock() {
		return false, nil
	}
	conn := h.conn
	h.mu.Unlock()
	if conn == nil || !conn.alive() {
		return false, nil
	}
	return true, op(conn)
}

func connectionLost(err error, conn *sshConn) bool {
	return err != nil && (!conn.alive() || errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, io.EOF))
}

func sshArgs(t target) []string {
	args := []string{"-T", "-x", "-e", "none", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}
	if t.url.User != nil && t.url.User.Username() != "" {
		args = append(args, "-l", t.url.User.Username())
	}
	if port := t.url.Port(); port != "" {
		args = append(args, "-p", port)
	}
	// -- ends the options, so a host can never be read as one.
	return append(args, "-s", "--", t.url.Hostname(), "sftp")
}

func dialSSH(ctx context.Context, t target) (*sshConn, error) {
	cmd := exec.Command(SSHCommand, sshArgs(t)...)
	// Questions are asked only while connecting.
	env, release := askpassEnv(ctx, t.url.Host)
	defer release()
	cmd.Env = env
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh: %w", err)
	}
	conn := &sshConn{cmd: cmd, stderr: stderr, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(conn.done)
	}()

	// The connection outlives this request, so ssh is not started with ctx;
	// cancelling stops only the wait for it to connect.
	type result struct {
		client *sftp.Client
		home   string
		err    error
	}
	ready := make(chan result, 1)
	go func() {
		client, err := sftp.NewClientPipe(stdout, stdin)
		if err != nil {
			ready <- result{err: err}
			return
		}
		home, err := client.Getwd()
		if err != nil {
			client.Close()
		}
		ready <- result{client: client, home: home, err: err}
	}()
	select {
	case r := <-ready:
		if r.err != nil {
			_ = cmd.Process.Kill()
			<-conn.done
			return nil, sshError(t, r.err, stderr.String())
		}
		conn.client, conn.home = r.client, r.home
		return conn, nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-conn.done
		return nil, ctx.Err()
	}
}

// sshError explains a failed connection, from what ssh said if it said
// anything: a refused key or an unknown host says more than a closed pipe.
func sshError(t target, err error, stderr string) error {
	host := t.url.Host
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		message := lines[len(lines)-1]
		if strings.Contains(message, "subsystem request failed") {
			message = "the server has no SFTP subsystem"
		}
		return fmt.Errorf("ssh %s: %s", host, message)
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		// SFTP that starts with garbage is usually a login script printing
		// something, which ssh passes on as if it came from the server.
		return fmt.Errorf("ssh %s: %v (does a login script such as ~/.bashrc print anything?)", host, err)
	}
	return fmt.Errorf("ssh %s: connection closed", host)
}

// resolve returns the remote path t names, with ~/ under the remote home.
func (c *sshConn) resolve(t target) string {
	if rest, ok := t.homeRelative(); ok {
		return path.Join(c.home, rest)
	}
	return t.url.Path
}

// homeURI is the URI for p, which is under the remote home.
func (c *sshConn) homeURI(t target, p string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(p, c.home), "/")
	return t.withPath(path.Join("/~", rest))
}

// statTarget finds the file t names. An absolute path that is not there is
// tried under the home too, since netrw and scp:// read sftp://host/file
// that way, and returns the home-relative URI it was found at.
func (c *sshConn) statTarget(t target) (string, fs.FileInfo, error) {
	p := c.resolve(t)
	info, err := c.client.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		if _, home := t.homeRelative(); !home {
			alt := path.Join(c.home, t.url.Path)
			if altInfo, altErr := c.client.Stat(alt); altErr == nil && !altInfo.IsDir() {
				return c.homeURI(t, alt), altInfo, nil
			}
		}
	}
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", Display(t.url.String()), err)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("%s is a directory", Display(t.url.String()))
	}
	return t.url.String(), info, nil
}

func fetchSFTP(ctx context.Context, t target, progress func(done, total int64)) (Info, error) {
	var info Info
	err := withSFTP(ctx, t, func(c *sshConn) error {
		uri, st, err := c.statTarget(t)
		if err != nil {
			return err
		}
		// Cached under the name it was found at, which the viewer goes on
		// to use.
		entry, err := entryFor(uri)
		if err != nil {
			return err
		}
		current := Info{URI: uri, ModTime: st.ModTime(), Size: st.Size()}
		if cached, ok := entry.cached(); ok && cached.SameVersion(current) {
			cached.URI = uri
			info = cached
			return nil
		}
		f, err := c.client.Open(c.resolve(mustTarget(uri)))
		if err != nil {
			return err
		}
		defer f.Close()
		stop := context.AfterFunc(ctx, func() { f.Close() })
		defer stop()
		info, err = entry.store(current, path.Base(uri), func(w io.Writer) error {
			_, err := f.WriteTo(&progressWriter{w: w, total: current.Size, progress: progress})
			if err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		})
		return err
	})
	return info, err
}

// mustTarget parses a URI this package made itself.
func mustTarget(uri string) target {
	t, err := parseTarget(uri)
	if err != nil {
		panic(err)
	}
	return t
}

func statSFTP(t target) (Info, bool, error) {
	var info Info
	connected, err := existingSFTP(t, func(c *sshConn) error {
		st, err := c.client.Stat(c.resolve(t))
		if err != nil {
			return err
		}
		info = Info{URI: t.url.String(), ModTime: st.ModTime(), Size: st.Size()}
		return nil
	})
	return info, connected, err
}

// uploadSFTP writes local over the file at t. The copy goes beside it under
// a temporary name and is renamed over it, so the file is never left half
// written. Unless force is set it refuses when the file is no longer the
// version opened, so someone else's change is not lost without asking.
func uploadSFTP(ctx context.Context, t target, local string, opened Info, force bool) (Info, error) {
	src, err := os.Open(local)
	if err != nil {
		return Info{}, err
	}
	defer src.Close()
	var info Info
	err = withSFTP(ctx, t, func(c *sshConn) error {
		// From the start on every attempt: a retry after a dropped
		// connection must not upload only what the first left unread.
		if _, err := src.Seek(0, io.SeekStart); err != nil {
			return err
		}
		dest := c.resolve(t)
		st, err := c.client.Stat(dest)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err == nil && !force && !opened.SameVersion(Info{ModTime: st.ModTime(), Size: st.Size()}) {
			return ErrConflict
		}
		tmp := path.Join(path.Dir(dest), "."+path.Base(dest)+".onon-"+randomSuffix())
		f, err := c.client.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return err
		}
		stop := context.AfterFunc(ctx, func() { f.Close() })
		_, err = f.ReadFrom(src)
		stop()
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil && st != nil {
			err = c.client.Chmod(tmp, st.Mode().Perm())
		}
		if err == nil {
			err = renameOver(c.client, tmp, dest)
		}
		if err != nil {
			_ = c.client.Remove(tmp)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		written, err := c.client.Stat(dest)
		if err != nil {
			return err
		}
		info = Info{URI: t.url.String(), Local: local, ModTime: written.ModTime(), Size: written.Size()}
		return nil
	})
	return info, err
}

// renameOver replaces dest with src, atomically where the server supports
// OpenSSH's POSIX rename. Plain SFTP rename will not replace a file.
func renameOver(client *sftp.Client, src, dest string) error {
	if _, ok := client.HasExtension("posix-rename@openssh.com"); ok {
		return client.PosixRename(src, dest)
	}
	if err := client.Remove(dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return client.Rename(src, dest)
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	if over := b.buf.Len() - b.max; over > 0 {
		b.buf.Next(over)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ErrConflict is returned when saving over a remote file that changed after
// it was opened.
var ErrConflict = errors.New("the remote file changed since it was opened")
