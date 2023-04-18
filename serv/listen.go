package serv

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/gobwas/ws"
	"github.com/sirupsen/logrus"
)

// Server websocket is a websocket server
type Server struct {
	sync.Mutex
	once    sync.Once
	id      string
	address string
	users   map[string]net.Conn
}

func NewServer(id, address string) *Server {
	return newServer(id, address)
}

func newServer(id, address string) *Server {
	return &Server{
		id:      id,
		address: address,
		users:   make(map[string]net.Conn, 100),
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	log := logrus.WithFields(logrus.Fields{
		"module": "Server",
		"listen": s.address,
		"id":     s.id,
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			conn.Close()
			return
		}
		// 读取userid
		user := r.URL.Query().Get("user")
		if user == "" {
			conn.Close()
			return
		}

		old, ok := s.addUser(user, conn)
		if ok {
			old.Close()
		}
		log.Infof("user %s in", user)

		go func(user string, conn net.Conn) {
			err := s.readloop(user, conn)
			if err != nil {
				conn.Close()
				return
			}
		}(user, conn)
	})
	log.Infoln("started")
	return http.ListenAndServe(s.address, mux)
}

func (s *Server) addUser(user string, conn net.Conn) (net.Conn, bool) {
	s.Lock()
	defer s.Unlock()

	oldUser, ok := s.users[user]
	s.users[user] = conn

	return oldUser, ok
}

func (s *Server) readloop(user string, conn net.Conn) error {
	for {
		frame, err := ws.ReadFrame(conn)
		if err != nil {
			return err
		}
		if frame.Header.OpCode == ws.OpClose {
			return errors.New("remote side close the conn")
		}

		if frame.Header.Masked {
			ws.Cipher(frame.Payload, frame.Header.Mask, 0)
		}
		// 接收文本帧内容
		if frame.Header.OpCode == ws.OpText {
			go s.handle(user, string(frame.Payload))
		}
	}
}

// 广播消息
func (s *Server) handle(user string, message string) {
	logrus.Infof("recv message %s from %s", message, user)
	s.Lock()
	defer s.Unlock()
	broadcast := fmt.Sprintf("%s -- FROM %s", message, user)
	for u, conn := range s.users {
		if u == user { // 不发给自己
			continue
		}
		logrus.Infof("send to %s : %s", u, broadcast)
		err := s.writeText(conn, broadcast)
		if err != nil {
			logrus.Errorf("write to %s failed, error: %v", user, err)
		}
	}
}

func (s *Server) writeText(conn net.Conn, message string) error {
	// 创建文本帧数据
	f := ws.NewTextFrame([]byte(message))
	return ws.WriteFrame(conn, f)
}
