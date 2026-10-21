package drpc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"flwengine/internal/nklog"
)

var (
	ErrUnsupported = errors.New("discord ipc unsupported on this platform")
	ErrOffline     = errors.New("discord ipc not connected")
)

type Client struct {
	mu       sync.Mutex
	log      *nklog.Writer
	conn     io.ReadWriteCloser
	clientID string
	lastAct  json.RawMessage
	lastHad  bool
}

func NewClient(log *nklog.Writer) *Client {
	return &Client{log: log}
}

func pipeCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		out := make([]string, 0, 10)
		for i := 0; i < 10; i++ {
			out = append(out, fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i))
		}
		return out
	case "linux", "darwin":
		dir := os.Getenv("XDG_RUNTIME_DIR")
		if dir == "" {
			dir = "/tmp"
		}
		out := make([]string, 0, 10)
		for i := 0; i < 10; i++ {
			out = append(out, fmt.Sprintf("%s/discord-ipc-%d", dir, i))
		}
		return out
	default:
		return nil
	}
}

func dialPipe() (io.ReadWriteCloser, error) {
	for _, cand := range pipeCandidates() {
		if runtime.GOOS == "windows" {
			f, err := os.OpenFile(cand, os.O_RDWR, 0)
			if err == nil {
				return f, nil
			}
			continue
		}
		c, err := net.DialTimeout("unix", cand, 1500*time.Millisecond)
		if err == nil {
			return c, nil
		}
	}
	return nil, ErrOffline
}

func writeFrame(w io.Writer, op uint32, payload []byte) error {
	head := make([]byte, 8)
	binary.LittleEndian.PutUint32(head[0:4], op)
	binary.LittleEndian.PutUint32(head[4:8], uint32(len(payload)))
	if _, err := w.Write(head); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func readFrame(r io.Reader) (uint32, []byte, error) {
	head := make([]byte, 8)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(head[0:4])
	n := binary.LittleEndian.Uint32(head[4:8])
	if n > 1<<20 {
		return 0, nil, errors.New("frame too large")
	}
	payload := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return op, payload, nil
}

func (c *Client) Supported() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "linux" || runtime.GOOS == "darwin"
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *Client) Login(clientID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return false
	}
	if c.conn != nil {
		if c.clientID == clientID {
			return true
		}
		c.conn.Close()
		c.conn = nil
	}
	if !c.Supported() {
		return false
	}
	conn, err := dialPipe()
	if err != nil {
		if c.log != nil {
			c.log.Debug("drpc", "ipc offline: %v", err)
		}
		return false
	}
	hs, _ := json.Marshal(map[string]any{"v": 1, "client_id": clientID})
	if err := writeFrame(conn, 1, hs); err != nil {
		conn.Close()
		return false
	}
	if dc, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		dc.SetReadDeadline(time.Now().Add(2500 * time.Millisecond))
	}
	op, payload, err := readFrame(conn)
	if err != nil || op != 1 {
		conn.Close()
		if c.log != nil {
			c.log.Debug("drpc", "handshake failed: %v", err)
		}
		return false
	}
	if dc, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		dc.SetReadDeadline(time.Time{})
	}
	var ready struct {
		Evt string `json:"evt"`
	}
	json.Unmarshal(payload, &ready)
	if ready.Evt != "READY" {
		conn.Close()
		return false
	}
	c.conn = conn
	c.clientID = clientID
	if c.log != nil {
		c.log.Info("drpc", "connected (client %s)", clientID)
	}
	if c.lastHad {
		c.sendActivityLocked(c.lastAct)
	}
	return true
}

func (c *Client) sendActivityLocked(activity json.RawMessage) error {
	if c.conn == nil {
		return ErrOffline
	}
	args := map[string]any{"pid": os.Getpid()}
	if len(activity) > 0 {
		args["activity"] = json.RawMessage(activity)
	}
	frame := map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  args,
		"nonce": fmt.Sprintf("flw-%d", time.Now().UnixNano()),
	}
	body, _ := json.Marshal(frame)
	return writeFrame(c.conn, 2, body)
}

func (c *Client) SetActivity(activity json.RawMessage) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastAct = activity
	c.lastHad = true
	if c.conn == nil {
		return false
	}
	if err := c.sendActivityLocked(activity); err != nil {
		c.conn.Close()
		c.conn = nil
		return false
	}
	return true
}

func (c *Client) Clear() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastAct = nil
	c.lastHad = false
	if c.conn == nil {
		return true
	}
	if err := c.sendActivityLocked(nil); err != nil {
		c.conn.Close()
		c.conn = nil
		return false
	}
	return true
}

func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}
