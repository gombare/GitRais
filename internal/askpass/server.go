// Package askpass forwards git/ssh credential prompts to the UI.
//
// Flow: git (or ssh) runs the askpass helper binary, which connects to a Unix
// socket served by this package and sends the prompt; the Handler (a UI dialog)
// answers, and the helper prints the answer on stdout.
//
// Interaction with credential helpers: Env does NOT disable credential helpers.
// The bundled git configures osxkeychain, which is consulted first; askpass runs
// only when no helper supplies the credential. Prompts and answers are never
// logged by this package.
//
// Keychain side effects: when git obtains a credential through askpass and the
// operation succeeds, git runs `credential approve`, which stores the typed
// password in the user's keychain via the bundled osxkeychain helper. When the
// server answers 401, git runs `credential reject`, which erases any existing
// keychain entry for that URL. Callers should be aware of both effects.
//
// Security model: the directory holding the socket is created with mode 0700
// and is the main protection. The token only guards against other users; any
// process of the same uid can read it from the environment of git and the
// helper, and could equally connect to the socket.
package askpass

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	EnvSock  = "GITRAIS_ASKPASS_SOCK"
	EnvToken = "GITRAIS_ASKPASS_TOKEN"
)

const (
	// maxRequest caps the size of one request line (token + prompt).
	maxRequest = 64 << 10
	// ioTimeout bounds reading the request and writing the response, so a stuck
	// client cannot pin a connection. It does not bound the Handler (the user
	// may take as long as needed to answer a dialog).
	ioTimeout = 10 * time.Second
)

// maxSockPath is the longest socket path used before falling back to /tmp
// (sun_path is 104 bytes on macOS, 108 on Linux).
const maxSockPath = 100

// Handler answers a prompt. ctx is cancelled when the Server is closed or when
// the requesting helper disconnects (e.g. the git operation was cancelled).
// ok=false means the user cancelled. Answers containing a line break are refused.
type Handler func(ctx context.Context, prompt string) (answer string, ok bool)

type Server struct {
	ln      net.Listener
	dir     string
	sock    string
	token   string
	handler Handler
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	mu    sync.Mutex
	conns map[net.Conn]struct{}

	closeOnce sync.Once
	closeErr  error
}

type request struct {
	Token  string `json:"token"`
	Prompt string `json:"prompt"`
}

type response struct {
	OK     bool   `json:"ok"`
	Answer string `json:"answer"`
}

func Start(h Handler) (*Server, error) {
	dir, err := os.MkdirTemp("", "gitrais-ap-") // created with mode 0700
	if err != nil {
		return nil, err
	}
	if len(filepath.Join(dir, "s")) > maxSockPath {
		// TMPDIR is too long for a Unix socket path: use a short fixed location.
		os.RemoveAll(dir)
		if dir, err = os.MkdirTemp("/tmp", "gitrais-ap-"); err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		ln.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{ln: ln, dir: dir, sock: sock, token: hex.EncodeToString(buf), handler: h, ctx: ctx, cancel: cancel, conns: map[net.Conn]struct{}{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		if !s.track(c) {
			c.Close()
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrack(c)
			s.serve(c)
		}()
	}
}

// track registers c so Close can interrupt it; it reports false once closing.
func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	c.Close()
}

func (s *Server) serve(c net.Conn) {
	c.SetReadDeadline(time.Now().Add(ioTimeout))
	// Read at most maxRequest bytes: a line that does not fit is rejected.
	line, err := bufio.NewReader(io.LimitReader(c, maxRequest)).ReadBytes('\n')
	var req request
	if err != nil || json.Unmarshal(line, &req) != nil {
		return
	}
	resp := response{}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.token)) == 1 {
		// The request is read: drop the read deadline and watch for the helper
		// going away, which cancels the per-request context.
		c.SetReadDeadline(time.Time{})
		ctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		go func() {
			var b [1]byte
			for {
				if _, err := c.Read(b[:]); err != nil {
					cancel()
					return
				}
			}
		}()
		resp = s.ask(ctx, req.Prompt)
	}
	b, _ := json.Marshal(resp)
	c.SetWriteDeadline(time.Now().Add(ioTimeout))
	c.Write(append(b, '\n'))
}

// ask runs the handler so that Close never waits for one that ignores its ctx
// (such a handler goroutine is abandoned and its answer discarded). A panicking
// handler or an answer with a line break is mapped to a refusal.
func (s *Server) ask(ctx context.Context, prompt string) response {
	ch := make(chan response, 1)
	go func() {
		defer func() {
			if recover() != nil {
				ch <- response{}
			}
		}()
		a, ok := s.handler(ctx, prompt)
		if strings.ContainsAny(a, "\r\n") {
			ch <- response{}
			return
		}
		ch <- response{OK: ok, Answer: a}
	}()
	select {
	case r := <-ch:
		if ctx.Err() != nil {
			return response{}
		}
		return r
	case <-ctx.Done():
		return response{}
	}
}

// Env returns the environment that makes git and ssh ask through helperPath.
func (s *Server) Env(helperPath string) []string {
	return []string{
		"GIT_ASKPASS=" + helperPath,
		"SSH_ASKPASS=" + helperPath,
		"SSH_ASKPASS_REQUIRE=force",
		"GIT_TERMINAL_PROMPT=0",
		EnvSock + "=" + s.sock,
		EnvToken + "=" + s.token,
	}
}

// Close stops the server, cancels pending handlers and removes the socket directory.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.closeErr = s.ln.Close()
		// Interrupt connections still waiting for a request.
		s.mu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
		os.RemoveAll(s.dir)
	})
	return s.closeErr
}
