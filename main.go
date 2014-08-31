package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512

	winnerThreshold = 1e8
)

type conn struct {
	ws   *websocket.Conn
	send chan []byte
}

func (c *conn) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(mt, payload)
}

func (c *conn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

type hub struct {
	conns      map[*conn]bool
	broadcast  chan []byte
	register   chan *conn
	unregister chan *conn
}

func (h *hub) run() {
	for {
		select {
		case c := <-h.register:
			h.conns[c] = true
		case c := <-h.unregister:
			if _, ok := h.conns[c]; ok {
				delete(h.conns, c)
				close(c.send)
			}
		case m := <-h.broadcast:
			for c := range h.conns {
				select {
				case c.send <- m:
				default:
					close(c.send)
					delete(h.conns, c)
				}
			}
		}
	}
}

type msg struct {
	Username string `json:"username"`
	Count    int64  `json:"count"`
	Winner   bool   `json:"winner"`
}

var (
	h = &hub{
		conns:      make(map[*conn]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *conn),
		unregister: make(chan *conn),
	}

	winningMsg *msg
	lastMsg    = &msg{}

	count int64
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	go h.run()

	http.HandleFunc("/", serveRoot)
	http.Handle("/static/",
		http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))
	http.HandleFunc("/yo", serveYo)
	http.HandleFunc("/connect", serveWs)

	http.ListenAndServe(":"+port, nil)
}

func serveRoot(w http.ResponseWriter, r *http.Request) {
	var msg *msg
	if winningMsg != nil {
		msg = winningMsg
	} else {
		msg = lastMsg
	}

	tmpl, err := template.New("index.html").ParseFiles("./index.html")
	if err != nil {
		log.Println(err)
		return
	}

	if err := tmpl.Execute(w, msg); err != nil {
		log.Println(err)
	}
}

func serveYo(w http.ResponseWriter, r *http.Request) {
	msg := &msg{}

	msg.Username = r.URL.Query().Get("username")
	msg.Count = count

	if count >= winnerThreshold && winningMsg == nil {
		msg.Winner = true
		msg.Count = winnerThreshold

		winningMsg = msg
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
	}

	h.broadcast <- msgBytes

	count++
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	if winningMsg != nil { // don't maintain ws after contest finished
		return
	}

	c := &conn{send: make(chan []byte, 256), ws: ws}
	h.register <- c
	c.writePump()
}
