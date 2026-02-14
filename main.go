package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

type AuthMessage struct {
	Type string `json:"type"`
}

type BotAuth struct {
	AuthMessage
	Password string `json:"password"`
	Name     string `json:"name"`
	ID       int    `json:"id"`
	CamID    string `json:"camId"`
	CamName  string `json:"camName"`
}

type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

func main() {
	u := url.URL{Scheme: "ws", Host: "localhost:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Send register event
	auth := &BotAuth{
		AuthMessage: AuthMessage{Type: "bot"}, // must match embedded field name
		Password:    "secret",
		Name:        "Bot123",
		ID:          123,
		CamID:       "cam123",
		CamName:     "Front Cam",
	}

	message := Message{
		Event: "registerBot",
		Data:  auth,
	}
	conn.WriteJSON(message)

	// Periodic telemetry
	for {
		telemetryMsg := Message{
			Event: "telemetry",
			Data: map[string]interface{}{
				"type":        "bot",
				"temperature": 24.5,
				"battery":     87,
			},
		}

		b, _ := json.Marshal(telemetryMsg)
		conn.WriteJSON(telemetryMsg)

		fmt.Println(string(b))

		time.Sleep(5 * time.Second)
	}
}
