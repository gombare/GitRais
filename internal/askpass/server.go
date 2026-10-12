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

// Handler answers a prompt. ctx is cancelled when the Server is closed.
// ok=false means the user cancelled.
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
		resp = s.ask(req.Prompt)
	}
	b, _ := json.Marshal(resp)
	c.SetWriteDeadline(time.Now().Add(ioTimeout))
	c.Write(append(b, '\n'))
}

// ask runs the handler so that Close never waits for one that ignores its ctx
// (such a handler goroutine is abandoned and its answer discarded).
func (s *Server) ask(prompt string) response {
	ch := make(chan response, 1)
	go func() {
		a, ok := s.handler(s.ctx, prompt)
		ch <- response{OK: ok, Answer: a}
	}()
	select {
	case r := <-ch:
		if s.ctx.Err() != nil {
			return response{}
		}
		return r
	case <-s.ctx.Done():
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
