package gamehost

import (
	"encoding/json"
	"net"
	"sync"
	"time"
)

type CtrlClient struct {
	mu    sync.Mutex
	conn  net.Conn
	addr  string
}

func NewCtrlClient(addr string) *CtrlClient {
	return &CtrlClient{addr: addr}
}

func (c *CtrlClient) connect() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return true
	}
	conn, err := net.DialTimeout("tcp", c.addr, 2*time.Second)
	if err != nil {
		return false
	}
	c.conn = conn
	return true
}

func (c *CtrlClient) Push(kind string, name string, data string) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		if !c.connect() {
			return
		}
		c.mu.Lock()
		conn = c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
	}
	msg := map[string]string{"type": kind, "name": name, "data": data}
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b = append(b, '\n')
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(b); err != nil {
		c.mu.Lock()
		c.conn.Close()
		c.conn = nil
		c.mu.Unlock()
	}
}

func (c *CtrlClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}
