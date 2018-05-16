package messenger

type RouterBridge struct {
	Node   *Node
	Router *Router
}

func StartBridge(address string, filters []string, router *Router) (*RouterBridge, error) {
	toReturn := &RouterBridge{
		Router: router,
		Node:   NewNode(address, filters),
	}

	return toReturn, toReturn.Node.ConnectToRouter(address)
}

func (r *RouterBridge) ReadPassthrough() {
	for {
		msg := r.Node.Read()
		r.Router.inChan <- msg
	}
}

func (r *RouterBridge) WritePassthrough(msg Message) {
	r.Node.Write(msg)
}
