package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"egovframe-launcher/internal/logbuf"
)

const rspServerID = "egovframe-tomcat"

type rspTypeRef struct {
	ID          string `json:"id"`
	VisibleName string `json:"visibleName,omitempty"`
	Description string `json:"description,omitempty"`
}

type rspHandle struct {
	ID   string     `json:"id"`
	Type rspTypeRef `json:"type"`
}

type rspClient struct {
	conn   net.Conn
	r      *bufio.Reader
	nextID int
}

func dialRSP(port int) (*rspClient, error) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &rspClient{conn: conn, r: bufio.NewReader(conn)}, nil
}

func (c *rspClient) send(method string, params any) (int, error) {
	c.nextID++
	id := c.nextID
	var p any = params
	if p == nil {
		p = json.RawMessage("null")
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  p,
	})
	if err != nil {
		return 0, err
	}
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := c.conn.Write([]byte(frame)); err != nil {
		return 0, err
	}
	if _, err := c.conn.Write(body); err != nil {
		return 0, err
	}
	return id, nil
}

type rspMsg struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Params json.RawMessage `json:"params"`
	Error  json.RawMessage `json:"error"`
}

// readMsg reads one framed JSON-RPC message, transparently answering
// server→client requests with null so callers only ever see responses
// (Method=="") and notifications (Method!="", ID==nil).
func (c *rspClient) readMsg(deadline time.Time) (rspMsg, error) {
	for {
		_ = c.conn.SetReadDeadline(deadline)
		// Read headers until blank line
		var contentLen int
		for {
			line, err := c.r.ReadString('\n')
			if err != nil {
				return rspMsg{}, err
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			const clHeader = "Content-Length: "
			if strings.HasPrefix(line, clHeader) {
				contentLen, _ = strconv.Atoi(strings.TrimPrefix(line, clHeader))
			}
		}
		if contentLen <= 0 {
			return rspMsg{}, fmt.Errorf("rsp: missing or zero Content-Length")
		}
		body := make([]byte, contentLen)
		if _, err := io.ReadFull(c.r, body); err != nil {
			return rspMsg{}, err
		}
		var msg rspMsg
		if err := json.Unmarshal(body, &msg); err != nil {
			return rspMsg{}, err
		}
		if msg.Method != "" && msg.ID != nil {
			c.replyNull(*msg.ID)
			continue
		}
		return msg, nil
	}
}

// replyNull sends a null response to a server-initiated request.
func (c *rspClient) replyNull(reqID int) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"result":  nil,
	})
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	_, _ = c.conn.Write([]byte(frame))
	_, _ = c.conn.Write(body)
}

func (c *rspClient) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	reqID, err := c.send(method, params)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		msg, err := c.readMsg(deadline)
		if err != nil {
			return nil, fmt.Errorf("rsp call %s: %w", method, err)
		}
		if msg.Method != "" {
			continue // notification
		}
		if msg.ID != nil && *msg.ID == reqID {
			if len(msg.Error) > 0 && string(msg.Error) != "null" {
				return nil, fmt.Errorf("rsp %s error: %s", method, string(msg.Error))
			}
			return msg.Result, nil
		}
	}
}

func (c *rspClient) waitState(serverID string, want int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		msg, err := c.readMsg(deadline)
		if err != nil {
			return fmt.Errorf("waitState timeout waiting for state %d: %w", want, err)
		}
		if msg.Method != "client/serverStateChanged" {
			continue
		}
		var p struct {
			Server struct {
				ID string `json:"id"`
			} `json:"server"`
			State int `json:"state"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if p.Server.ID != serverID {
			continue
		}
		if p.State == want {
			return nil
		}
		if p.State == 4 { // STOPPED unexpectedly
			return fmt.Errorf("server %s stopped unexpectedly", serverID)
		}
	}
}

// rspStopServer asks the RSP backend to stop the shared Tomcat server and
// waits until it reports STOPPED.
func rspStopServer(port int, serverID string, logs *logbuf.Buf) error {
	c, err := dialRSP(port)
	if err != nil {
		return fmt.Errorf("RSP 연결 실패: %w", err)
	}
	defer c.conn.Close()
	logs.Append("[info] RSP stopServerAsync id=" + serverID)
	if _, err := c.call("server/stopServerAsync", map[string]any{"id": serverID, "force": false}, 15*time.Second); err != nil {
		logs.Append("[warn] RSP stopServerAsync: " + err.Error() + " (계속)")
	}
	if err := c.waitState(serverID, 4, 30*time.Second); err != nil {
		return fmt.Errorf("RSP 서버 정지 확인 실패: %w", err)
	}
	logs.Append("[success] RSP 서버 STOPPED 확인")
	return nil
}

// rspPublish re-runs a FULL publish (kind=2) for serverID. Used to recover
// from the webapps copy race: Tomcat's autoDeploy can start a context while
// RSP is still copying the exploded WAR, leaving it failed with missing
// classes — a second publish updates timestamps and triggers a clean redeploy.
func rspPublish(port int, serverID string, logs *logbuf.Buf) error {
	c, err := dialRSP(port)
	if err != nil {
		return fmt.Errorf("RSP 연결 실패: %w", err)
	}
	defer c.conn.Close()
	handle := rspHandle{ID: serverID}
	if res, err := c.call("server/getServerHandles", nil, 15*time.Second); err == nil {
		var handles []rspHandle
		_ = json.Unmarshal(res, &handles)
		for _, h := range handles {
			if h.ID == serverID {
				handle = h
				break
			}
		}
	}
	logs.Append("[info] RSP publish kind=2 (재시도)")
	_, err = c.call("server/publish", map[string]any{"server": handle, "kind": 2}, 15*time.Second)
	return err
}

// rspDeployAndStart runs the proven RSP sequence:
// createServer → getServerHandles → addDeployable → startServerAsync → waitState(STARTED) → publish
func rspDeployAndStart(port int, serverID, typeID, tomcatHome, label, explodedPath string, logs *logbuf.Buf) error {
	c, err := dialRSP(port)
	if err != nil {
		return fmt.Errorf("RSP 연결 실패: %w", err)
	}
	defer c.conn.Close()

	callTimeout := 15 * time.Second

	// 1. createServer
	logs.Append(fmt.Sprintf("[info] RSP createServer id=%s typeID=%s", serverID, typeID))
	createParams := map[string]any{
		"id":         serverID,
		"serverType": typeID,
		"attributes": map[string]any{
			"server.home.dir": filepath.ToSlash(tomcatHome),
		},
	}
	createResult, err := c.call("server/createServer", createParams, callTimeout)
	if err != nil {
		// Non-fatal: server may already exist
		logs.Append("[warn] RSP createServer: " + err.Error() + " (무시하고 계속)")
	} else {
		var cr struct {
			Status struct {
				Severity int `json:"severity"`
			} `json:"status"`
		}
		if json.Unmarshal(createResult, &cr) == nil && cr.Status.Severity != 0 {
			logs.Append(fmt.Sprintf("[warn] RSP createServer severity=%d (무시하고 계속)", cr.Status.Severity))
		}
	}

	// 2. getServerHandles — find our handle to reuse the real type object
	logs.Append("[info] RSP getServerHandles")
	handlesResult, err := c.call("server/getServerHandles", nil, callTimeout)
	if err != nil {
		return fmt.Errorf("getServerHandles 실패: %w", err)
	}
	var handles []rspHandle
	_ = json.Unmarshal(handlesResult, &handles)

	handle := rspHandle{
		ID:   serverID,
		Type: rspTypeRef{ID: typeID},
	}
	for _, h := range handles {
		if h.ID == serverID {
			handle = h
			break
		}
	}

	// 3. addDeployable
	logs.Append(fmt.Sprintf("[info] RSP addDeployable label=%s path=%s", label, explodedPath))
	addParams := map[string]any{
		"server": handle,
		"deployableReference": map[string]any{
			"label": label,
			"path":  filepath.ToSlash(explodedPath),
		},
	}
	addResult, err := c.call("server/addDeployable", addParams, callTimeout)
	if err != nil {
		logs.Append("[warn] RSP addDeployable: " + err.Error() + " (무시하고 계속)")
	} else {
		var ar struct {
			Severity int `json:"severity"`
		}
		if json.Unmarshal(addResult, &ar) == nil && ar.Severity != 0 {
			logs.Append(fmt.Sprintf("[warn] RSP addDeployable severity=%d (무시하고 계속)", ar.Severity))
		}
	}

	// 4. startServerAsync
	logs.Append("[info] RSP startServerAsync")
	startParams := map[string]any{
		"mode": "run",
		"params": map[string]any{
			"id":         serverID,
			"serverType": handle.Type.ID,
			"attributes": map[string]any{},
		},
	}
	_, err = c.call("server/startServerAsync", startParams, callTimeout)
	if err != nil {
		logs.Append("[warn] RSP startServerAsync: " + err.Error() + " (이미 기동 중일 수 있음, 계속)")
	}

	// Wait for STARTED (state==2) notification
	logs.Append("[info] RSP 서버 기동 대기 중 (최대 45초)...")
	if err := c.waitState(serverID, 2, 45*time.Second); err != nil {
		// RSP's own STARTED notification can be delayed or lost after an
		// external process restart (e.g. a port change), even though Tomcat
		// itself comes up fine — this is non-fatal, not a failure signal.
		logs.Append("[info] RSP STARTED 알림 지연 (서버는 정상 기동 중일 수 있음, 계속 진행)")
	} else {
		logs.Append("[info] RSP 서버 STARTED 확인")
	}

	// 5. publish kind=2 (FULL)
	logs.Append("[info] RSP publish kind=2")
	publishParams := map[string]any{
		"server": handle,
		"kind":   2,
	}
	_, err = c.call("server/publish", publishParams, callTimeout)
	if err != nil {
		return fmt.Errorf("RSP publish 실패: %w", err)
	}

	logs.Append("[success] RSP publish 완료")
	return nil
}
