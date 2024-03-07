package PusherClientGo

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Subscription struct {
	channel string
}

type Callback func(data PusherMessage)

type PusherClient struct {
	//socket
	connectionString string
	socketId         *string
	conn             *websocket.Conn

	// Subscriptions
	subscriptions map[string]Subscription

	// Callbacks
	callbacks map[string]Callback

	// Auth
	AuthFunc *PusherClientAuthFunc

	//mutex
	mutex *sync.Mutex

	//Context
	ctx    context.Context
	cancel context.CancelFunc
}

type PusherClientAuthFunc func(channel string, socketId string) string

type PusherClientConfig struct {
	ConnectionString string
	//This is a pointer to a function that takes in a channel and socketId and returns an auth string
	AuthFunc PusherClientAuthFunc
}

func NewPusherClient(config *PusherClientConfig) *PusherClient {
	pusherClient := PusherClient{
		connectionString: config.ConnectionString,
		subscriptions:    make(map[string]Subscription),
		callbacks:        make(map[string]Callback),
		AuthFunc:         &config.AuthFunc,
		mutex:            &sync.Mutex{},
	}

	return &pusherClient
}

func (pusherClient *PusherClient) Connect() error {
	pusherClient.mutex.Lock()

	pusherClient.ctx, pusherClient.cancel = context.WithCancel(context.Background())
	c, _, err := websocket.DefaultDialer.Dial(pusherClient.connectionString, nil)
	if err != nil {
		return err
	}

	pusherClient.conn = c
	pusherClient.mutex.Unlock()
	go pusherClient.read(pusherClient.ctx)

	return nil
}

func (pusherClient *PusherClient) Subscribe(channel string, callback Callback, authNeeded bool) error {
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()

	_, ok := pusherClient.subscriptions[channel]
	if ok {
		return nil
	}

	pusherClient.subscriptions[channel] = Subscription{
		channel: channel,
	}

	pusherClient.callbacks[channel] = callback

	if pusherClient.socketId != nil {
		pusherClient.handleSendSubscription(pusherClient.subscriptions[channel])
	}

	return nil
}

func (pusherClient *PusherClient) Unsubscribe(channel string) error {
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()

	_, ok := pusherClient.subscriptions[channel]
	if !ok {
		return nil
	}

	delete(pusherClient.subscriptions, channel)

	unsubscribeMessage := pusherClient.getUnsubscribeChatMessage(channel)

	err := pusherClient.conn.WriteJSON(unsubscribeMessage)
	if err != nil {
		return err
	}

	return nil
}

func (pusherClient *PusherClient) handleSendSubscription(sub Subscription) {
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()
	auth := ""

	// We need to get auth if the user provided an auth function
	if pusherClient.AuthFunc != nil {
		auth = (*pusherClient.AuthFunc)(sub.channel, *pusherClient.socketId)
	}

	subscriptionMessage := pusherClient.getSubscribeChatMessage(sub.channel, auth)

	pusherClient.conn.WriteJSON(subscriptionMessage)
}

func (pusherClient *PusherClient) handleSendSubscriptions() {
	for channel := range pusherClient.subscriptions {
		pusherClient.handleSendSubscription(pusherClient.subscriptions[channel])
	}

}

func (pusherClient *PusherClient) handleReconnect() {
	// Initialize the delay for exponential falloff
	delay := 1 * time.Second
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()
	if pusherClient.conn != nil {
		pusherClient.conn.Close()
		pusherClient.conn = nil
	}
	pusherClient.mutex.Unlock()

	for {
		// Attempt to reconnect to the socket
		err := pusherClient.Connect()
		if err == nil {
			break
		}

		// If the reconnection fails, wait for the delay period and then double the delay for the next attempt
		time.Sleep(delay)
		delay *= 2

		// Cap the delay at 1 minute to prevent extremely long waiting times
		if delay > 1*time.Minute {
			delay = 1 * time.Minute
		}
	}

	// Once the socket is reconnected, send all subscriptions again
	pusherClient.handleSendSubscriptions()
}

func (pusherClient *PusherClient) read(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			messageType, message, err := pusherClient.conn.ReadMessage()
			if err != nil {
				pusherClient.handleReconnect()
				break
			}
			if messageType == websocket.TextMessage {
				var pm PusherMessage
				json.Unmarshal(message, &pm)

				pusherClient.mutex.Lock()
				defer pusherClient.mutex.Unlock()
				switch pm.Event {
				case "pusher:connection_established":
					var parsedData ConnectionMessage
					json.Unmarshal([]byte(pm.Data), &parsedData)
					pusherClient.socketId = &parsedData.SocketId
					pusherClient.handleSendSubscriptions()
				}

				callback, ok := pusherClient.callbacks[pm.Channel]
				if ok {
					callback(pm)
				}
				pusherClient.mutex.Unlock()
			}
		}
	}
}

func (pusherClient *PusherClient) Close() {
	if pusherClient.cancel != nil {
		pusherClient.cancel() // Signal the read goroutine to stop
	}
	// Close the WebSocket connection and other cleanup...
	pusherClient.conn.Close()
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()
	pusherClient.conn = nil
}
