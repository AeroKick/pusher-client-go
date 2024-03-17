package PusherClientGo

func (pusherClient *PusherClient) getSubscribeChatMessage(channel string, auth string) PusherSubscriptionMessage {
	return PusherSubscriptionMessage{
		Event: "pusher:subscribe",
		Data: struct {
			Auth    string `json:"auth"`
			Channel string `json:"channel"`
		}{
			Auth:    auth,
			Channel: channel,
		},
	}
}

func (pusherClient *PusherClient) getUnsubscribeChatMessage(channel string) PusherUnsubscribeMessage {
	return PusherUnsubscribeMessage{
		Event: "pusher:unsubscribe",
		Data: struct {
			Channel string `json:"channel"`
		}{
			Channel: channel,
		},
	}
}
