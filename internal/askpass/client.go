package askpass

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

// Ask sends prompt to the server at sock and returns the answer. It fails if the
// user cancelled or the token is wrong. There is no read deadline on the answer:
// the user may take as long as needed; a dead server closes the connection.
func Ask(sock, token, prompt string) (string, error) {
	c, err := net.DialTimeout("unix", sock, ioTimeout)
	if err != nil {
		return "", err
	}
	defer c.Close()
	b, err := json.Marshal(request{Token: token, Prompt: prompt})
	if err != nil {
		return "", err
	}
	c.SetWriteDeadline(time.Now().Add(ioTimeout))
	if _, err := c.Write(append(b, '\n')); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(io.LimitReader(c, maxRequest)).ReadBytes('\n')
	if err != nil {
		return "", err
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return "", err
	}
	if !resp.OK {
		return "", errors.New("credential request cancelled")
	}
	return resp.Answer, nil
}
