package messenger

type Message struct {
	Header string `json:"header"` // Header is the event type
	Body   []byte `json:"body"`   // Body can be whatever message is desired.
}
