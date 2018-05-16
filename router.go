package messenger

import (
	"log"

	"github.com/fatih/color"
)

type Router struct {
	unregister        chan *Subscription
	register          chan *Subscription
	inChan            chan Message
	outChan           chan Message
	subscriptions     map[*Subscription]bool
	routingTable      map[string][]string
	routerConnections map[*RouterBridge]bool
	showMessageLogs   bool
}

func NewRouter() *Router {
	return &Router{
		inChan:            make(chan Message, 1024),
		outChan:           make(chan Message, 1024),
		register:          make(chan *Subscription),
		unregister:        make(chan *Subscription),
		subscriptions:     make(map[*Subscription]bool),
		routingTable:      make(map[string][]string),
		routerConnections: make(map[*RouterBridge]bool),
	}
}

func (r *Router) SetMessageLogs(value bool) {
	r.showMessageLogs = value
}

func (r *Router) StartRouter(RoutingTable map[string][]string) error {
	r.showMessageLogs = false
	r.routingTable = RoutingTable

	for {
		select {
		case sub := <-r.register:
			r.subscriptions[sub] = true

		case sub := <-r.unregister:
			if _, ok := r.subscriptions[sub]; ok {
				delete(r.subscriptions, sub)
				close(sub.send)
			}
		case message := <-r.inChan:
			//we need to run it through the routing table
			r.route(message)
		}
	}
}

//ConnectToRouters takes a list of peer routers to connect to.
func (r *Router) ConnectToRouters(peerAddresses []string, RoutingTable map[string][]string) error {

	//build our filters
	filters := []string{}

	for k := range RoutingTable {
		filters = append(filters, k)
	}
	log.Printf(color.YellowString("filters: %v", filters))

	for _, addr := range peerAddresses {
		log.Printf(color.BlueString("Connecting to peer Event Router: %v", addr))

		go func(addr string, filters []string) {
			bridge, err := StartBridge(addr, filters, r)
			if err != nil {
				log.Printf(color.HiRedString("Could not establish connection to the peer %v", addr))
				return
			}
			log.Printf(color.BlueString("Done connecting to peer %v", addr))

			go bridge.ReadPassthrough()
			r.routerConnections[bridge] = true
		}(addr, filters)
	}

	return nil
}

func (r *Router) Stop() error {
	return nil
}

func (r *Router) route(message Message) {
	if r.showMessageLogs {
		log.Printf(color.HiYellowString("Routing a message with header: %v", message.Header))
	}

	headers, ok := r.routingTable[message.Header]
	if !ok {
		//it's not a message we care about
		return
	}

	if r.showMessageLogs {
		log.Printf(color.HiYellowString("We care.", message.Header))
	}
	for _, newHeader := range headers {
		if r.showMessageLogs {
			log.Printf(color.HiYellowString("Routing to %v", newHeader))
		}
		for sub := range r.subscriptions {
			if r.showMessageLogs {
				log.Printf(color.HiYellowString("sending to %v", sub.conn.RemoteAddr().String()))
			}
			select {
			case sub.send <- Message{Body: message.Body, Header: newHeader}:
			default:
				close(sub.send)
				delete(r.subscriptions, sub)
			}
		}

		for routerConn := range r.routerConnections {
			if r.showMessageLogs {
				log.Printf(color.HiYellowString("sending to router %v", routerConn.Node.conn.RemoteAddr().String()))
			}
			routerConn.WritePassthrough(Message{Body: message.Body, Header: newHeader})
		}

	}
}

func (r *Router) GetInfo() map[string]interface{} {

	states := make(map[string]interface{})

	for v := range r.routerConnections {
		k, val := v.Node.GetState()
		states[k] = val
	}

	return states
}
