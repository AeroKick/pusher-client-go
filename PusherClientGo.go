package PusherClientGo

import (
	"context"
	"encoding/json"
	"log"
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
	mutex *sync.RWMutex

	//Context
	ctx    context.Context
	cancel context.CancelFunc

	// Debug
	debug bool
}

type PusherClientAuthFunc func(channel string, socketId string) string

type PusherClientConfig struct {
	ConnectionString string
	//This is a pointer to a function that takes in a channel and socketId and returns an auth string
	AuthFunc PusherClientAuthFunc
	Debug    bool
}

func NewPusherClient(config *PusherClientConfig) *PusherClient {
	if config.Debug {
		log.Println("Creating new PusherClient")
	}
	pusherClient := PusherClient{
		connectionString: config.ConnectionString,
		subscriptions:    make(map[string]Subscription),
		callbacks:        make(map[string]Callback),
		AuthFunc:         &config.AuthFunc,
		mutex:            &sync.RWMutex{},
		debug:            config.Debug,
	}

	return &pusherClient
}

func (pusherClient *PusherClient) Connect() error {
	if pusherClient.debug {
		log.Println("Connecting to Pusher")
	}
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()

	pusherClient.ctx, pusherClient.cancel = context.WithCancel(context.Background())
	c, _, err := websocket.DefaultDialer.Dial(pusherClient.connectionString, nil)
	if err != nil {
		return err
	}

	pusherClient.conn = c
	go pusherClient.read(pusherClient.ctx)

	return nil
}

func (pusherClient *PusherClient) Subscribe(channel string, callback Callback, authNeeded bool) error {
	if pusherClient.debug {
		log.Printf("Subscribing to channel: %s", channel)
	}

	// Lock only to check and possibly add to subscriptions map
	pusherClient.mutex.Lock()
	subscription, ok := pusherClient.subscriptions[channel]
	if ok {
		pusherClient.mutex.Unlock()
		return nil
	}

	// Create a new subscription and add it to the map
	subscription = Subscription{channel: channel}
	pusherClient.subscriptions[channel] = subscription
	pusherClient.mutex.Unlock()

	// Lock only to add to callbacks map
	pusherClient.mutex.Lock()
	pusherClient.callbacks[channel] = callback
	pusherClient.mutex.Unlock()

	// Handle sending the subscription if socketId is not nil
	if pusherClient.socketId != nil {
		pusherClient.handleSendSubscription(subscription)
	}

	return nil
}

func (pusherClient *PusherClient) Unsubscribe(channel string) error {
	if pusherClient.debug {
		log.Printf("Unsubscribing from channel: %s", channel)
	}
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
	if pusherClient.debug {
		log.Printf("Sending subscription for channel: %s", sub.channel)
	}
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()
	auth := ""

	// We need to get auth if the user provided an auth function
	if pusherClient.AuthFunc != nil {
		auth = (*pusherClient.AuthFunc)(sub.channel, *pusherClient.socketId)
	}

	subscriptionMessage := pusherClient.getSubscribeChatMessage(sub.channel, auth)

	pusherClient.conn.WriteJSON(subscriptionMessage)
	if pusherClient.debug {
		log.Printf("Sent subscription for channel: %s", sub.channel)
	}
}

func (pusherClient *PusherClient) handleSendSubscriptions() {
	if pusherClient.debug {
		log.Println("Sending all subscriptions")
	}
	for channel := range pusherClient.subscriptions {
		pusherClient.handleSendSubscription(pusherClient.subscriptions[channel])
	}
}

func (pusherClient *PusherClient) handleReconnect() {
	if pusherClient.debug {
		log.Println("Handling reconnect")
	}
	// Initialize the delay for exponential falloff
	delay := 1 * time.Second
	pusherClient.mutex.Lock()
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
			if pusherClient.debug {
				log.Println("Stopping read loop due to context cancellation")
			}
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

				switch pm.Event {
				case "pusher:connection_established":
					var parsedData ConnectionMessage
					json.Unmarshal([]byte(pm.Data), &parsedData)
					pusherClient.mutex.Lock()
					pusherClient.socketId = &parsedData.SocketId
					pusherClient.mutex.Unlock()
					pusherClient.handleSendSubscriptions()
				}

				pusherClient.mutex.Lock()
				callback, ok := pusherClient.callbacks[pm.Channel]
				pusherClient.mutex.Unlock()
				if ok {
					callback(pm)
				}
			}
		}
	}
}

func (pusherClient *PusherClient) Close() {
	if pusherClient.debug {
		log.Println("Closing PusherClient connection")
	}
	if pusherClient.cancel != nil {
		pusherClient.cancel() // Signal the read goroutine to stop
	}
	// Close the WebSocket connection and other cleanup...
	pusherClient.conn.Close()
	pusherClient.mutex.Lock()
	defer pusherClient.mutex.Unlock()
	pusherClient.conn = nil
}
