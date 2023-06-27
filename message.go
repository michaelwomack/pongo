package main

type MessageType int

var (
	MessageTypeInvalid              = MessageType(0)
	MessageTypeClientConnected      = MessageType(1)
	MessageTypeGameState            = MessageType(2)
	MessageTypePlayerInput          = MessageType(3)
	MessageTypeGameStartCountdown   = MessageType(4)
	MessageTypeOpponentDisconnected = MessageType(5)
)

type GameState struct {
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	Ball             *Ball   `json:"ball"`
	Me               *Client `json:"me"`
	Opponent         *Client `json:"opponent"`
	Collision        bool    `json:"collision"`
	Streak           int     `json:"streak"`
	SecondsRemaining int64   `json:"secondsRemaining"`
}

type MessageGameState struct {
	Type  MessageType `json:"type"`
	State GameState   `json:"state"`
}

type MessageGameStartCountdown struct {
	Type    MessageType `json:"type"`
	Counter int         `json:"counter"`
}

type MessageClientConnected struct {
	Type MessageType `json:"type"`
	Me   *Client     `json:"me"`
}

type MessageOpponentDisconnected struct {
	Type MessageType `json:"type"`
}

func NewMessageClientConnected(client *Client) MessageClientConnected {
	return MessageClientConnected{
		Type: MessageTypeClientConnected,
		Me:   client,
	}
}

type MessagePlayerInput struct {
	Type   MessageType `json:"type"`
	Paddle *Paddle     `json:"paddle"`
}
