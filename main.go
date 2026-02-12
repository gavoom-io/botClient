package main

import (
	"encoding/json"
	"log"
	"net/url"
	"time"
	"fmt"

	"github.com/gorilla/websocket"
)
type AuthMessage struct {
	Type string `json:"type"`
}

type BotAuth struct {
	AuthMessage
	Password string `json:"password"`
	Name string`json:"name"`
	ID int `json: "id"`
	CamID string`json: "camId"`
	CamName string `json: "camName"`
}

type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

func main() {
	u := url.URL{Scheme: "ws", Host: "192.168.0.141:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Send register event
	auth := BotAuth {
		AuthMessage: AuthMessage{Type: "bot"},
		Password: "secret",
		Name: "Bot123",
		ID: 123,
		CamID: "cam123",
		CamName: "Front Cam",
	}


	

	// Periodic telemetry
	for {
		//telemetryMsg := Message{
		//	Event: "telemetry",
		//	Data: map[string]interface{}{
		//		"temperature": 24.5,
		//		"battery":     87,
		//	},
		//}

		b, _ := json.Marshal(auth)
		
		fmt.Println(string(b))
		conn.WriteJSON(auth)

		time.Sleep(5 * time.Second)
	}
}
