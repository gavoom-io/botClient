package main

import (
	"encoding/json"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v4"
)

type AuthMessage struct {
	Type string `json:"type"`
}

type WSMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type BotAuth struct {
	AuthMessage
	Password string `json:"password"`
	Name     string `json:"name"`
	ID       string `json:"id"`
	CamID    string `json:"camId"`
	CamName  string `json:"camName"`
}

type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

type WebRTCOfferData struct {
	AuthMessage AuthMessage               `json:"auth"`
	SDP         webrtc.SessionDescription `json:"sdp"`
	UserId      string                    `json:"userId"`
	BotId       string                    `json:"botId"`
}

type SDPPayload struct {
	UserID string `json:"userId"`
	SDP    struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	} `json:"sdp"`
}

type ICECandidateMessage struct {
	BotID     string                  `json:"botId"`
	UserID    string                  `json:"userId"`
	Candidate webrtc.ICECandidateInit `json:"candidate"`
}

var botId = "82065db3-003d-4e08-9e20-136bb089d795"
var currentPC *webrtc.PeerConnection
var currentStream mediadevices.MediaStream
var  blocked bool

func cleanupPeerConnection(pc *webrtc.PeerConnection, stream mediadevices.MediaStream) {
    if pc != nil {
        if err := pc.Close(); err != nil {
            log.Println("pc close error:", err)
        }
        pc = nil
    }

    if stream != nil {
        for _, t := range stream.GetVideoTracks() {
            t.Close() // releases the camera
            log.Println("stopped video track:", t.ID())
        }
        for _, t := range stream.GetAudioTracks() {
            t.Close()
            log.Println("stopped audio track:", t.ID())
        }
        stream = nil
    }
}

func handleRequestOffer(conn *websocket.Conn, data json.RawMessage, botId string,ch chan ICECandidateMessage ) {
	 if blocked == true {
          return
         }
         blocked = true
          cleanupPeerConnection(currentPC,currentStream);
         var payload SDPPayload
         pc, _, err := createPeerConnectionAndStream()
       
        pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		msg := ICECandidateMessage{
			BotID:     botId,
			Candidate: c.ToJSON(),
		}
		b, _ := json.Marshal(msg)
		conn.WriteJSON(WSMessage{Event: "iceCandidate", Data: b})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("PC state:", state)
	})
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Println("failed to unmarshal sdp offer:", err)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.NewSDPType(payload.SDP.Type),
		SDP:  payload.SDP.SDP,
	}

	if err := pc.SetRemoteDescription(offer); err != nil {
		log.Println("SetRemoteDescription error:", err)
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Println("CreateAnswer error:", err)
		return
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		log.Println("SetLocalDescription error:", err)
		return
	}

        go func() {
        for msg := range ch {
           
	    if err := pc.AddICECandidate(msg.Candidate); err != nil {
		log.Println("AddICECandidate error:", err)
	     }
}
        
    }()

	conn.WriteJSON(Message{
		Event: "deviceAnswer",
		Data: WebRTCOfferData{
			AuthMessage: AuthMessage{Type: "bot"},
			UserId:      payload.UserID,
			BotId:       botId,
			SDP:         *pc.LocalDescription(),
		},
	})
       blocked = false
}

func createPeerConnectionAndStream() (*webrtc.PeerConnection, mediadevices.MediaStream, error) {
    // Create PeerConnection
    vp8Params, err := vpx.NewVP8Params()
	if err != nil {
		return nil, nil, err
	}
	vp8Params.BitRate = 500_000

	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(&vp8Params),
	)

	m := &webrtc.MediaEngine{}
	codecSelector.Populate(m)

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		return nil,nil, err
	}
        
	 
        var stream mediadevices.MediaStream

	stream, err = mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.Width = prop.Int(320)
			c.Height = prop.Int(240)
			c.FrameRate = prop.Float(15)
		},
		Codec: codecSelector,
	})
        
         for _, t := range stream.GetVideoTracks() {
		if _, err := pc.AddTrack(t); err != nil {
			return nil,nil,err
		}
	}

     currentPC = pc
     currentStream = stream

    return pc, stream, nil
}


func run() error {
         //-- ice chan //
        iceChan := make(chan ICECandidateMessage, 20)

	// --- WebSocket ---
	u := url.URL{Scheme: "ws", Host: "192.168.0.141:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
        defer func() {
        conn.Close()
        log.Println("WebSocket disconnected")
        close(iceChan) // close the channel safely
        }()

	log.Println("websocket connected")

	// register bot
	auth := &BotAuth{
		AuthMessage: AuthMessage{Type: "bot"},
		Password:    "secret",
		Name:        "test",
		ID:          botId,
		CamID:       "cam123",
		CamName:     "Front Cam",
	}

	conn.WriteJSON(Message{Event: "registerBot", Data: auth})

	// --- Read loop ---
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("ws.ReadMessage:", err)
			return err // ✅ triggers reconnect
		}

		var ws WSMessage
		if err := json.Unmarshal(msgBytes, &ws); err != nil {
			log.Println("unmarshal WS message:", err)
			continue
		}

		switch ws.Event {

		case "requestOffer":
			handleRequestOffer(conn, ws.Data, auth.ID, iceChan)

		case "ice":
			var msg ICECandidateMessage
                        if err := json.Unmarshal(ws.Data, &msg); err != nil {
				log.Println("failed ICE:", err)
				continue
			}
                        iceChan <- msg 

		case "userDisconnected":
			log.Println("user disconnected → closing PC")
                        //cleanupPeerConnection(pc,stream)
			return nil // restart session cleanly
		}
	}
}

func main() {
	for {
		log.Println("starting session...")

		err := run()

		log.Println("session ended:", err)

		time.Sleep(2 * time.Second)
	}
}
