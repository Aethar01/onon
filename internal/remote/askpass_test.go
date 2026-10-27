package remote

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"onon/internal/remote/remotetest"
)

// usePrompter answers ssh's questions with answer for the rest of the test,
// recording what was asked.
func usePrompter(t *testing.T, answer func(ctx context.Context, p Prompt) (string, bool)) *[]Prompt {
	t.Helper()
	var mu sync.Mutex
	asked := &[]Prompt{}
	SetPrompter(func(ctx context.Context, p Prompt) (string, bool) {
		mu.Lock()
		*asked = append(*asked, p)
		mu.Unlock()
		return answer(ctx, p)
	})
	t.Cleanup(func() {
		SetPrompter(nil)
		closeBroker()
	})
	return asked
}

// askThrough runs the askpass side in this process with env, as ssh would
// run it, and returns what it printed and its exit code.
func askThrough(env []string, question string) (string, int) {
	vars := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		vars[k] = v
	}
	var out bytes.Buffer
	code := runAskpass([]string{question}, func(k string) string { return vars[k] }, &out, io.Discard)
	return out.String(), code
}

func TestAskpassAnswersFromThePrompter(t *testing.T) {
	asked := usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) {
		if p.Kind == PromptText {
			return "yes", true
		}
		return "hunter2", true
	})
	env, release := askpassEnv(context.Background(), "host:2222")
	defer release()
	vars := strings.Join(env, "\n")
	for _, want := range []string{"SSH_ASKPASS=", "SSH_ASKPASS_REQUIRE=force", askpassAddrEnv + "=", askpassTokenEnv + "="} {
		if !strings.Contains(vars, want) {
			t.Fatalf("env lacks %s", want)
		}
	}

	if out, code := askThrough(env, "me@host's password: "); code != 0 || out != "hunter2\n" {
		t.Fatalf("password answer = %q, exit %d", out, code)
	}
	hostKey := "The authenticity of host 'host (10.0.0.1)' can't be established.\nED25519 key fingerprint is SHA256:abc.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? "
	if out, code := askThrough(env, hostKey); code != 0 || out != "yes\n" {
		t.Fatalf("host key answer = %q, exit %d", out, code)
	}
	want := []Prompt{
		{Host: "host:2222", Text: "me@host's password", Kind: PromptSecret},
		{Host: "host:2222", Text: "The authenticity of host 'host (10.0.0.1)' can't be established. ED25519 key fingerprint is SHA256:abc. Are you sure you want to continue connecting (yes/no/[fingerprint])?", Kind: PromptText},
	}
	if len(*asked) != len(want) || (*asked)[0] != want[0] || (*asked)[1] != want[1] {
		t.Fatalf("asked %+v", *asked)
	}
}

func TestAskpassRefusesWithoutASession(t *testing.T) {
	usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) { return "secret", true })
	env, release := askpassEnv(context.Background(), "host")
	release()
	if out, code := askThrough(env, "password: "); code == 0 || out != "" {
		t.Fatalf("after release: %q, exit %d", out, code)
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, askpassTokenEnv+"=") {
			env[i] = askpassTokenEnv + "=forged"
		}
	}
	if _, code := askThrough(env, "password: "); code == 0 {
		t.Fatal("a wrong token was answered")
	}
}

func TestAskpassQuestionEndsWithTheAttempt(t *testing.T) {
	waiting := make(chan struct{})
	usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) {
		close(waiting)
		<-ctx.Done()
		return "", false
	})
	env, release := askpassEnv(context.Background(), "host")
	done := make(chan int)
	go func() {
		_, code := askThrough(env, "password: ")
		done <- code
	}()
	<-waiting
	release()
	select {
	case code := <-done:
		if code == 0 {
			t.Fatal("an abandoned question was answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the question outlived the connection attempt")
	}
}

func TestNoPrompterLeavesSSHOnTheTerminal(t *testing.T) {
	SetPrompter(nil)
	if env, release := askpassEnv(context.Background(), "host"); env != nil {
		release()
		t.Fatal("askpass set up without a prompter")
	}
}

func TestSSHAsksThroughThePrompter(t *testing.T) {
	useTempCache(t)
	home, _ := useFakeSSH(t)
	if err := os.WriteFile(home+"/a.pdf", []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	remotetest.RequireAnswer(t, "me@host's password: ", "hunter2")

	asked := usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) { return "wrong", true })
	_, err := Fetch(context.Background(), "sftp://me@host/~/a.pdf", nil)
	if err == nil || !strings.Contains(err.Error(), "Permission denied") || len(*asked) != 1 {
		t.Fatalf("wrong password: err=%v asked=%+v", err, *asked)
	}

	usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) { return "hunter2", true })
	if _, err := Fetch(context.Background(), "sftp://me@host/~/a.pdf", nil); err != nil {
		t.Fatal(err)
	}
}
