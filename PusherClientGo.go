package PusherClientGo

import (
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
	socketId         string
	conn             *websocket.Conn

	// Subscriptions
	subscriptions map[string]Subscription

	// Callbacks
	callbacks map[string]Callback

	// Auth
	AuthFunc *PusherClientAuthFunc

	//mutex
	mutex *sync.Mutex
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
	c, _, err := websocket.DefaultDialer.Dial(pusherClient.connectionString, nil)
	if err != nil {
		return err
	}

	pusherClient.mutex.Lock()
	pusherClient.conn = c
	pusherClient.mutex.Unlock()
	go pusherClient.read()

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

	auth := ""

	//We need to get auth if the user provided an auth function
	if pusherClient.AuthFunc != nil && authNeeded {
		auth = (*pusherClient.AuthFunc)(channel, pusherClient.socketId)
	}

	subscriptionMessage := pusherClient.getSubscribeChatMessage(channel, auth)

	err := pusherClient.conn.WriteJSON(subscriptionMessage)
	if err != nil {
		return err
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

func (pusherClient *PusherClient) handleReconnect() {
	// Initialize the delay for exponential falloff
	delay := 1 * time.Second

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

	pusherClient.mutex.Lock()
	for channel := range pusherClient.subscriptions {
		auth := ""

		// We need to get auth if the user provided an auth function
		if pusherClient.AuthFunc != nil {
			auth = (*pusherClient.AuthFunc)(channel, pusherClient.socketId)
		}

		subscriptionMessage := pusherClient.getSubscribeChatMessage(channel, auth)

		err := pusherClient.conn.WriteJSON(subscriptionMessage)
		if err != nil {
			// If the subscription fails, attempt to handle reconnection again
			pusherClient.handleReconnect()
		}
	}
	pusherClient.mutex.Unlock()
}

func (pusherClient *PusherClient) read() {
	for {
		messageType, message, err := pusherClient.conn.ReadMessage()
		if err != nil {
			pusherClient.handleReconnect()
			break
		}
		if messageType == websocket.TextMessage {
			var pm PusherMessage
			json.Unmarshal(message, &pm)

			pusherClient.mutex.Lock()
			switch pm.Event {
			case "pusher:connection_established":
				var parsedData ConnectionMessage
				json.Unmarshal([]byte(pm.Data), &parsedData)
				pusherClient.socketId = parsedData.SocketId
			}

			callback, ok := pusherClient.callbacks[pm.Channel]
			if ok {
				callback(pm)
			}
			pusherClient.mutex.Unlock()
		}
	}
}
