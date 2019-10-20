package cmd

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/blueworrybear/benno-proxy/sockets"
	huproxy "github.com/google/huproxy/lib"
	"github.com/gorilla/websocket"
	"github.com/urfave/cli"
)

var CmdClient = cli.Command{
	Name:   "client",
	Action: runClient,
}
var (
	dialTimeout      = flag.Duration("dial_timeout", 10*time.Second, "Dial timeout.")
	handshakeTimeout = flag.Duration("handshake_timeout", 10*time.Second, "Handshake timeout.")
	writeTimeout     = flag.Duration("write_timeout", 10*time.Second, "Write timeout.")
)

func runClient(ctx *cli.Context) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	go runSocket()
	<-signalChan
	req, _ := http.NewRequest("DELETE", "http://localhost:8080/socket/bear", nil)
	http.DefaultClient.Do(req)
}

func runSocket() {
	c, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/socket/bear", nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer func() {
		c.Close()
	}()
	_, msg, err := c.ReadMessage()
	go runSocket()
	if err != nil {
		log.Fatalln(err)
		return
	}
	req, err := sockets.DecodeRequest(msg)
	if err != nil {
		log.Fatalln(err)
		return
	}
	newReq, _ := http.NewRequest(req.Method, req.URL.String(), bytes.NewReader(req.Body))
	newReq.Header = req.Header

	if req.Method == "CONNECT" {
		handleTunnel(newReq, c)
	} else {
		handleProxy(newReq, c)
	}
}

func handleProxy(req *http.Request, c *websocket.Conn) {
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalln(err)
		return
	}
	data, err := sockets.EncodeResponse(res)
	if err != nil {
		log.Fatalln(err)
		return
	}
	c.WriteMessage(websocket.BinaryMessage, data)
}

func handleTunnel(req *http.Request, c *websocket.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	destCon, _ := net.DialTimeout("tcp", req.URL.Host, *dialTimeout)
	defer destCon.Close()
	go func() {
		for {
			mt, r, err := c.NextReader()
			if websocket.IsCloseError(err,
				websocket.CloseNormalClosure,   // Normal.
				websocket.CloseAbnormalClosure, // OpenSSH killed proxy client.
			) {
				return
			}
			if err != nil {
				log.Printf("nextreader: %v\n", err)
				return
			}
			if mt != websocket.BinaryMessage {
				log.Println("blah")
				return
			}
			if _, err := io.Copy(destCon, r); err != nil {
				log.Printf("Reading from websocket: %v", err)
				cancel()
			}
		}
	}()

	if err := huproxy.File2WS(ctx, cancel, destCon, c); err == io.EOF {
		log.Println("Close Socket...")
	} else if err != nil {
		log.Printf("Reading from file: %v", err)
	}
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	io.Copy(destination, source)
}
