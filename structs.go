package PusherClientGo

type ConnectionMessage struct {
	SocketId string `json:"socket_id"`
}

type PusherMessage struct {
	Event   string `json:"event"`
	Data    string `json:"data"`
	Channel string `json:"channel"`
}

type PusherSubscriptionMessage struct {
	Event string `json:"event"`
	Data  struct {
		Auth    string `json:"auth"`
		Channel string `json:"channel"`
	} `json:"data"`
}

type PusherUnsubscribeMessage struct {
	Event string `json:"event"`
	Data  struct {
		Channel string `json:"channel"`
	} `json:"data"`
}
