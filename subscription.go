package messenger

import (
	"log"
	"net/http"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Time allowed to read the next pong message from the router.
	pingWait = 90 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 5) / 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type Subscription struct {
	router *Router
	conn   *websocket.Conn
	send   chan Message
}

func ListenForNodes(router *Router, resp http.ResponseWriter, req *http.Request) error {
	conn, err := upgrader.Upgrade(resp, req, nil)
	if err != nil {
		log.Println(err)
		return err
	}

	subscription := &Subscription{router: router, conn: conn, send: make(chan Message, 1024)}
	subscription.router.register <- subscription

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go subscription.writePump()
	subscription.readPump()

	return nil
}

func (s *Subscription) readPump() {
	defer func() {
		log.Printf(color.HiBlueString("[%v] read pump closing", s.conn.RemoteAddr()))
		s.router.unregister <- s
		s.conn.Close()
	}()

	s.conn.SetReadDeadline(time.Now().Add(pongWait))
	s.conn.SetPongHandler(func(string) error {
		log.Printf(color.HiCyanString("[%v] Pongo old boy!", s.conn.RemoteAddr()))
		s.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		var message Message
		err := s.conn.ReadJSON(&message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Printf("error: %v", err)
			}
			break
		}
		s.router.inChan <- message
	}
}

func (s *Subscription) writePump() {
	log.Printf(color.BlueString("Starting write pump with a ping timer of %v", pingPeriod))
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()
		s.conn.Close()
	}()

	for {
		select {
		case message, ok := <-s.send:
			s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				s.conn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(writeWait))
				return
			}

			err := s.conn.WriteJSON(message)
			if err != nil {
				return
			}

		case <-ticker.C:
			s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			log.Printf(color.HiCyanString("[%v] I'm hungry mother. I'm hungry", s.conn.RemoteAddr().String()))
			if err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}
