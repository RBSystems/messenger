package messenger

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
)

const (
	// Interval to wait between retry attempts
	retryInterval = 3 * time.Second
)

type Node struct {
	Name string

	routerAddress string
	conn          *websocket.Conn
	writeQueue    chan Message
	readQueue     chan Message
	filters       map[string]bool
	readDone      chan bool
	writeDone     chan bool
	lastPingTime  time.Time
	state         string
}

func NewNode(name string, filters []string) *Node {
	// create the node
	n := &Node{
		Name:       name,
		state:      "standby",
		readQueue:  make(chan Message, 4096),
		writeQueue: make(chan Message, 4096),
		readDone:   make(chan bool, 1),
		writeDone:  make(chan bool, 1),
		filters:    make(map[string]bool),
	}

	// add all the filters
	for _, f := range filters {
		n.filters[f] = true
	}

	return n
}

func (n *Node) ConnectToRouter(address string) error {
	n.routerAddress = address

	// open connection with router
	err := n.openConnection()
	if err != nil {
		log.Printf(color.YellowString("Opening connection failed, retrying..."))

		n.readDone <- true
		n.writeDone <- true
		go n.retryConnection()

		return errors.New(fmt.Sprintf("failed to open connection to router %v. retrying connection...", n.routerAddress))
	}

	// update state to good
	n.state = "good"
	log.Printf(color.HiGreenString("Successfully connected node %s to %s. Starting pumps...", n.Name, address))

	// start read/write pumps
	go n.readPump()
	go n.writePump()

	return nil
}

func (n *Node) GetState() (string, interface{}) {
	values := make(map[string]interface{})

	values["router"] = n.routerAddress

	if n.conn != nil {
		values["connection"] = fmt.Sprintf("%v => %v", n.conn.LocalAddr().String(), n.conn.RemoteAddr().String())
	} else {
		values["connection"] = fmt.Sprintf("%v => %v", "local", n.routerAddress)
	}

	filters := []string{}
	for filter := range n.filters {
		filters = append(filters, filter)
	}

	values["filters"] = filters
	values["state"] = n.state
	values["last-ping-time"] = n.lastPingTime.Format(time.RFC3339)
	return n.Name, values
}

func (n *Node) openConnection() error {
	// open connection to the router
	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(fmt.Sprintf("ws://%s/subscribe", n.routerAddress), nil)
	if err != nil {
		return errors.New(fmt.Sprintf("failed opening websocket with %v: %s", n.routerAddress, err))
	}

	n.conn = conn
	return nil
}

func (n *Node) retryConnection() {
	// mark the connection as 'down'
	n.state = n.state + " retrying"

	log.Printf(color.HiMagentaString("[retry] Retrying connection, waiting for read and write pump to close before starting."))
	//wait for read to say i'm done.
	<-n.readDone
	log.Printf(color.HiMagentaString("[retry] Read pump closed"))

	//wait for write to be done.
	<-n.writeDone
	log.Printf(color.HiMagentaString("[retry] Write pump closed"))
	log.Printf(color.HiMagentaString("[retry] Retrying connection"))

	//we retry
	err := n.openConnection()

	for err != nil {
		log.Printf(color.HiMagentaString("[retry] Retry failed, trying again in 3 seconds."))
		time.Sleep(retryInterval)
		err = n.openConnection()
	}

	//start the pumps again
	log.Printf(color.HiGreenString("[Retry] Retry success. Starting pumps"))

	n.state = "good"
	go n.readPump()
	go n.writePump()

}

func (n *Node) readPump() {
	defer func() {
		n.conn.Close()
		log.Printf(color.HiRedString("Connection to router %v is dying.", n.routerAddress))
		n.state = "down"

		n.readDone <- true
	}()

	n.conn.SetPingHandler(
		func(string) error {
			log.Printf(color.HiCyanString("[%v] Ping! Ping was my best friend growin' up.", n.routerAddress))
			n.conn.SetReadDeadline(time.Now().Add(pingWait))
			n.conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(writeWait))

			//debugging purposes
			n.lastPingTime = time.Now()

			return nil
		})

	n.conn.SetReadDeadline(time.Now().Add(pingWait))

	for {
		var message Message
		err := n.conn.ReadJSON(&message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Printf("error: %v", err)
			}
			break
		}

		_, ok := n.filters[message.Header]
		if !ok {
			continue
		}

		//we need to check against the list of accepted values
		n.readQueue <- message
	}
}

func (n *Node) writePump() {
	defer func() {
		n.conn.Close()
		log.Printf(color.HiRedString("Connection to router %v is dying. Trying to resurrect.", n.routerAddress))
		n.state = "down"

		n.writeDone <- true

		//try to reconnect
		n.retryConnection()
	}()

	for {
		select {
		case message, ok := <-n.writeQueue:
			if !ok {
				n.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(writeWait))
				return
			}

			err := n.conn.WriteJSON(message)
			if err != nil {
				return
			}
		case <-n.readDone:
			// put it back in
			n.readDone <- true
			return
		}
	}
}

func (n *Node) Write(message Message) error {
	n.writeQueue <- message
	return nil
}

func (n *Node) Read() Message {
	return <-n.readQueue
}

func (n *Node) Close() {
}
