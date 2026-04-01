package main

import (
"encoding/json"
"log"
"net/url"

"github.com/gorilla/websocket"
"github.com/pion/mediadevices"
_ "github.com/pion/mediadevices/pkg/driver/camera"
"github.com/pion/mediadevices/pkg/codec/vpx"
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

func must(err error) {
if err != nil {
panic(err)
}
}

var botId = "09eb14ea-6b5f-42ce-b5ca-83e24ef5a828"

func cleanupPeerConnection(pc *webrtc.PeerConnection) {
    if pc == nil {
        return
    }

    // Close the PeerConnection
    err := pc.Close()
    if err != nil {
        log.Println("pc close error:", err)
    }

    // Optional: nil it so you don't reuse it
    pc = nil
}

func handleRequestOffer(pc *webrtc.PeerConnection, conn *websocket.Conn, data json.RawMessage, botId string) {
if pc == nil {
log.Println("ERROR: pc is nil!")
return
}

var payload SDPPayload
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


conn.WriteJSON(Message{
Event: "deviceAnswer",
Data: WebRTCOfferData{
AuthMessage: AuthMessage{Type: "bot"},
UserId:      payload.UserID,
BotId:       botId,
SDP:         *pc.LocalDescription(),
},
})
}

func main() {
// enumerate devices
devices := mediadevices.EnumerateDevices()
for _, d := range devices {
log.Printf("device: %s | kind: %v | id: %s\n", d.Label, d.Kind, d.DeviceID)
}

// codec setup
vp8Params, err := vpx.NewVP8Params()
must(err)
vp8Params.BitRate = 500_000
codecSelector := mediadevices.NewCodecSelector(
mediadevices.WithVideoEncoders(&vp8Params),
)
m := &webrtc.MediaEngine{}
codecSelector.Populate(m)

// peer connection
api := webrtc.NewAPI(webrtc.WithMediaEngine(m))
pc, err := api.NewPeerConnection(webrtc.Configuration{
ICEServers: []webrtc.ICEServer{
{URLs: []string{"stun:stun.l.google.com:19302"}},
},
})
must(err)

// camera
stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
Video: func(c *mediadevices.MediaTrackConstraints) {
c.Width = prop.Int(320)
c.Height = prop.Int(240)
c.FrameRate = prop.Float(15)
},
Codec: codecSelector,
})
must(err)
log.Println("got user media, tracks:", len(stream.GetVideoTracks()))

for _, t := range stream.GetVideoTracks() {
_, err := pc.AddTrack(t)
must(err)
log.Println("added track:", t.ID())
}

// websocket — declared here so OnICECandidate closure can use it
u := url.URL{Scheme: "ws", Host: "192.168.0.141:47000", Path: "/ws"}
conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
must(err)
log.Println("websocket connected")

// ICE handler after conn is ready
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

   switch state {
	case webrtc.PeerConnectionStateDisconnected:
             log.Println("Peer disconnected")
  }



})

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
log.Println("bot registered, waiting for offers...")

conn.SetCloseHandler(func(code int, text string) error {
    log.Printf("closed with code=%d, reason=%s\n", code, text)
    return nil
})

// message loop
for {
_, msgBytes, err := conn.ReadMessage()
if err != nil {
log.Println("ws.ReadMessage:", err)
 conn.Close()
 cleanupPeerConnection(pc)
  continue
}

var ws WSMessage
if err := json.Unmarshal(msgBytes, &ws); err != nil {
log.Println("unmarshal WS message:", err)
continue
}

log.Println("received event:", ws.Event)

switch ws.Event {
case "requestOffer":
handleRequestOffer(pc, conn, ws.Data, auth.ID)
case "ice":
var msg ICECandidateMessage
if err := json.Unmarshal(ws.Data, &msg); err != nil {
log.Println("failed to unmarshal ICE:", err)
continue
}
if err := pc.AddICECandidate(msg.Candidate); err != nil {
log.Println("AddICECandidate error:", err)
} else {
log.Println("added ICE candidate")
}
case "control":
var ctrl map[string]string
json.Unmarshal(ws.Data, &ctrl)
log.Println("control command:", ctrl["command"])

case "userDisconnected":
  cleanupPeerConnection(pc)
}
}
}
