package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"wdev/internal/terminal"

	"github.com/gorilla/websocket"
)

const (
	termWriteWait = 10 * time.Second
	termPongWait  = 60 * time.Second
	termPingEvery = 25 * time.Second
)

func (s *Server) ensureTerminalManager() *terminal.Manager {
	if s.termMgr == nil {
		s.termMgr = terminal.New(s.cfg, s.store)
	}
	return s.termMgr
}

// TerminalManager mengembalikan manajer PTY terminal milik server.
func (s *Server) TerminalManager() *terminal.Manager {
	return s.ensureTerminalManager()
}

func (s *Server) registerTerminalRoutes() {
	s.mux.HandleFunc("/api/terminal/info", s.handleTerminalInfo)
	s.mux.HandleFunc("/api/terminal/ws", s.handleTerminalWS)
}

func (s *Server) handleTerminalInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	info := s.ensureTerminalManager().SystemInfo()
	if s.pdnsMgr != nil {
		info.ServerMode = s.pdnsMgr.GetMode()
	}
	writeJSON(w, http.StatusOK, info)
}

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 32768,
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && strings.EqualFold(u.Host, r.Host)
	},
}

type wsClientControlMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	Data string `json:"data,omitempty"`
}

func parseQueryUint16(q url.Values, key string, fallback uint16) uint16 {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseUint(raw, 10, 16)
	if err != nil {
		return fallback
	}
	return uint16(v)
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	q := r.URL.Query()
	opts := terminal.StartOptions{
		Cwd:   q.Get("cwd"),
		Shell: q.Get("shell"),
		Cols:  parseQueryUint16(q, "cols", terminal.DefaultCols),
		Rows:  parseQueryUint16(q, "rows", terminal.DefaultRows),
	}

	var wsMu sync.Mutex
	sendTextJSON := func(payload any) error {
		b, merr := json.Marshal(payload)
		if merr != nil {
			return merr
		}
		wsMu.Lock()
		defer wsMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(termWriteWait))
		return conn.WriteMessage(websocket.TextMessage, b)
	}
	sendBinary := func(data []byte) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(termWriteWait))
		return conn.WriteMessage(websocket.BinaryMessage, data)
	}

	sess, fallbackReason, err := s.ensureTerminalManager().StartSession(opts)
	if err != nil {
		_ = sendTextJSON(map[string]any{
			"type":    "error",
			"message": err.Error(),
		})
		return
	}
	defer sess.Close()

	_ = sendTextJSON(map[string]any{
		"type":            "ready",
		"session_id":      sess.ID,
		"cwd":             sess.Cwd,
		"shell":           sess.Shell,
		"user":            sess.User,
		"host":            sess.Hostname,
		"fallback_reason": fallbackReason,
	})

	banner := fmt.Sprintf(
		"\x1b[1;36mwdev PTY SSH Shell\x1b[0m \x1b[90m•\x1b[0m \x1b[32m%s@%s\x1b[0m \x1b[90m•\x1b[0m shell: \x1b[33m%s\x1b[0m\r\n\x1b[90mWorking Dir:\x1b[0m %s\r\n",
		sess.User, sess.Hostname, sess.Shell, sess.Cwd,
	)
	if fallbackReason != "" {
		banner += fmt.Sprintf("\x1b[33mCatatan:\x1b[0m %s\r\n", fallbackReason)
	}
	banner += "\r\n"
	_ = sendBinary([]byte(banner))

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(termPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(termPongWait))
		return nil
	})

	doneCh := make(chan struct{})
	var doneOnce sync.Once
	stop := func() {
		doneOnce.Do(func() {
			close(doneCh)
		})
	}

	// Keepalive Ping untuk menjaga koneksi di belakang proxy / Cloudflare
	go func() {
		ticker := time.NewTicker(termPingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				wsMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(termWriteWait))
				perr := conn.WriteMessage(websocket.PingMessage, nil)
				wsMu.Unlock()
				if perr != nil {
					stop()
					return
				}
			case <-doneCh:
				return
			}
		}
	}()

	// Pompa PTY stdout/stderr -> WebSocket
	go func() {
		defer stop()
		buf := make([]byte, 32*1024)
		for {
			n, rerr := sess.Read(buf)
			if n > 0 {
				if werr := sendBinary(buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				if rerr != io.EOF {
					// Abaikan EIO wajar saat slave PTY ditutup ketika proses shell keluar.
					_ = rerr
				}
				code, _ := sess.Wait()
				_ = sendTextJSON(map[string]any{
					"type": "exit",
					"code": code,
				})
				wsMu.Lock()
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
				wsMu.Unlock()
				return
			}
		}
	}()

	// Pompa WebSocket stdin/control -> PTY
	go func() {
		defer stop()
		for {
			msgType, data, rerr := conn.ReadMessage()
			if rerr != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(termPongWait))

			switch msgType {
			case websocket.BinaryMessage:
				if len(data) > 0 {
					if _, werr := sess.Write(data); werr != nil {
						return
					}
				}
			case websocket.TextMessage:
				var ctrl wsClientControlMsg
				if jerr := json.Unmarshal(data, &ctrl); jerr != nil {
					continue
				}
				switch ctrl.Type {
				case "resize":
					_ = sess.Resize(ctrl.Cols, ctrl.Rows)
				case "input":
					if ctrl.Data != "" {
						if _, werr := sess.Write([]byte(ctrl.Data)); werr != nil {
							return
						}
					}
				case "ping":
					_ = sendTextJSON(map[string]string{"type": "pong"})
				}
			}
		}
	}()

	<-doneCh
}
