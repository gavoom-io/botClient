package main

import (
	"encoding/json"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

func main() {
	u := url.URL{Scheme: "ws", Host: "localhost:8084", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Send register event
	registerMsg := Message{
		Event: "register",
		Data: map[string]string{
			"botId": "bot-123",
			"token": "secret",
		},
	}

	b, _ := json.Marshal(registerMsg)
	conn.WriteMessage(websocket.TextMessage, b)

	// Periodic telemetry
	for {
		telemetryMsg := Message{
			Event: "telemetry",
			Data: map[string]interface{}{
				"temperature": 24.5,
				"battery":     87,
			},
		}

		b, _ := json.Marshal(telemetryMsg)
		conn.WriteMessage(websocket.TextMessage, b)

		time.Sleep(5 * time.Second)
	}
}
