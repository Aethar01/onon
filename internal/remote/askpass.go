package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// onon is its own SSH_ASKPASS, so ssh asks for passwords, passphrases and
// unknown host keys in the window, on every system and wherever onon was
// started from. ssh runs the onon binary with the question as its argument;
// that process, finding the environment below, hands the question over a
// socket to the onon that started ssh and prints the answer for ssh. Each
// connection attempt gets its own token, so a question can only reach the
// window while the attempt that asks it is still going.

const (
	askpassAddrEnv  = "ONON_ASKPASS_SOCKET"
	askpassTokenEnv = "ONON_ASKPASS_TOKEN"
)

// PromptKind is the sort of answer ssh wants.
type PromptKind int

const (
	PromptSecret  PromptKind = iota // a password or passphrase, typed hidden
	PromptText                      // a visible answer, such as yes to a new host key
	PromptConfirm                   // allow or refuse, such as an agent key use
	PromptNotice                    // nothing to answer: a message, such as to touch a security key
)

// Prompt is one question from ssh, or from a web server's login.
type Prompt struct {
	Host    string
	Text    string
	Kind    PromptKind
	Initial string // an answer to offer, such as a user name already known
}

// A Prompter answers ssh's questions, returning ok false to refuse. It must
// give up when ctx ends, which it does once the connection attempt is over.
type Prompter func(ctx context.Context, prompt Prompt) (answer string, ok bool)

var (
	prompterMu sync.Mutex
	prompter   Prompter
)

// SetPrompter sets where ssh's questions go. With none, ssh asks on the
// terminal, as it does when run by hand.
func SetPrompter(p Prompter) {
	prompterMu.Lock()
	prompter = p
	prompterMu.Unlock()
}

func currentPrompter() Prompter {
	prompterMu.Lock()
	defer prompterMu.Unlock()
	return prompter
}

type askpassRequest struct {
	Token  string `json:"token"`
	Prompt string `json:"prompt"`
	Hint   string `json:"hint,omitempty"` // SSH_ASKPASS_PROMPT
}

type askpassReply struct {
	OK     bool   `json:"ok"`
	Answer string `json:"answer,omitempty"`
}

// RunAskpass answers for ssh when this process was started as its askpass,
// and reports false otherwise. main calls it before anything else.
func RunAskpass() (exitCode int, ran bool) {
	if os.Getenv(askpassAddrEnv) == "" {
		return 0, false
	}
	return runAskpass(os.Args[1:], os.Getenv, os.Stdout, os.Stderr), true
}

func runAskpass(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	conn, err := net.Dial("unix", getenv(askpassAddrEnv))
	if err != nil {
		fmt.Fprintln(stderr, "onon askpass:", err)
		return 1
	}
	defer conn.Close()
	request := askpassRequest{Token: getenv(askpassTokenEnv), Prompt: strings.Join(args, " "), Hint: getenv("SSH_ASKPASS_PROMPT")}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		fmt.Fprintln(stderr, "onon askpass:", err)
		return 1
	}
	var reply askpassReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil || !reply.OK {
		return 1
	}
	fmt.Fprintln(stdout, reply.Answer)
	return 0
}

// askpassBroker listens for the askpass processes ssh starts.
type askpassBroker struct {
	ln   net.Listener
	dir  string
	addr string

	mu       sync.Mutex
	sessions map[string]askpassSession
}

type askpassSession struct {
	ctx  context.Context
	host string
}

var (
	brokerMu sync.Mutex
	broker   *askpassBroker
)

func getBroker() (*askpassBroker, error) {
	brokerMu.Lock()
	defer brokerMu.Unlock()
	if broker != nil {
		return broker, nil
	}
	// A directory of its own, readable only by this user, keeps the socket
	// from anyone else; the token keeps it from stale ssh processes.
	dir, err := os.MkdirTemp("", "onon-askpass-")
	if err != nil {
		return nil, err
	}
	addr := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", addr)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	broker = &askpassBroker{ln: ln, dir: dir, addr: addr, sessions: map[string]askpassSession{}}
	go broker.serve()
	return broker, nil
}

func closeBroker() {
	brokerMu.Lock()
	b := broker
	broker = nil
	brokerMu.Unlock()
	if b != nil {
		b.ln.Close()
		os.RemoveAll(b.dir)
	}
}

// askpassEnv is the environment for an ssh connecting to host, which sends
// its questions to the prompter until release is called or ctx ends. With
// no prompter it is nil, and ssh uses the terminal.
func askpassEnv(ctx context.Context, host string) (env []string, release func()) {
	p := currentPrompter()
	if p == nil {
		return nil, func() {}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, func() {}
	}
	b, err := getBroker()
	if err != nil {
		return nil, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	token := randomSuffix() + randomSuffix() + randomSuffix() + randomSuffix()
	b.mu.Lock()
	b.sessions[token] = askpassSession{ctx: ctx, host: host}
	b.mu.Unlock()
	env = append(os.Environ(),
		"SSH_ASKPASS="+exe,
		"SSH_ASKPASS_REQUIRE=force", // OpenSSH 8.4 and later: even with a terminal
		askpassAddrEnv+"="+b.addr,
		askpassTokenEnv+"="+token,
	)
	if runtime.GOOS != "windows" && os.Getenv("DISPLAY") == "" {
		// Older ssh uses askpass only with a display, which onon does not
		// need; X11 forwarding is off, so the name is not used.
		env = append(env, "DISPLAY=onon:0")
	}
	return env, func() {
		cancel()
		b.mu.Lock()
		delete(b.sessions, token)
		b.mu.Unlock()
	}
}

func (b *askpassBroker) serve() {
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			return
		}
		go b.handle(conn)
	}
}

func (b *askpassBroker) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var request askpassRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	b.mu.Lock()
	session, known := b.sessions[request.Token]
	b.mu.Unlock()
	p := currentPrompter()
	reply := askpassReply{}
	if known && p != nil {
		ctx, cancel := context.WithCancel(session.ctx)
		// The askpass going away, as when ssh is killed, ends the question.
		go func() {
			_, _ = io.Copy(io.Discard, io.MultiReader(decoder.Buffered(), conn))
			cancel()
		}()
		reply.Answer, reply.OK = p(ctx, Prompt{Host: session.host, Text: cleanPrompt(request.Prompt), Kind: promptKind(request)})
		cancel()
	}
	_ = json.NewEncoder(conn).Encode(reply)
}

func promptKind(request askpassRequest) PromptKind {
	switch request.Hint {
	case "confirm":
		return PromptConfirm
	case "none":
		return PromptNotice
	}
	// ssh asks to accept a new host key as an ordinary question, to be
	// answered yes, no or with the fingerprint.
	if strings.Contains(request.Prompt, "(yes/no") {
		return PromptText
	}
	return PromptSecret
}

// cleanPrompt puts ssh's question on one line, for the status bar, without
// the colon the prompt adds.
func cleanPrompt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(strings.TrimSuffix(text, ":"))
}
