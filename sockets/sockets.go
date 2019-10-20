package sockets

import (
	"bytes"
	"context"
	"encoding/gob"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-collections/go-datastructures/queue"
	huproxy "github.com/google/huproxy/lib"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}
var (
	writeTimeout = flag.Duration("timeout", 10*time.Second, "Write timeout.")
)

// Sockets hold Sockets
type Sockets struct {
	pool map[string]*SocketPool
}

// Socket holds user's websocket connection
type SocketPool struct {
	User string
	Pool *queue.Queue
}

type Socket struct {
	Conn *websocket.Conn
	Wait sync.WaitGroup
}

type Request struct {
	Method string
	URL    *url.URL
	Header http.Header
	Body   []byte
}

type Response struct {
	Status     string
	StatusCode int
	Header     http.Header
	Body       []byte
}

// NewSockets create a new Sockets
func NewSockets() *Sockets {
	s := &Sockets{pool: make(map[string]*SocketPool)}
	return s
}

// RegistNewUser creates a new Socekt and websocket connection
func (sockets *Sockets) RegistNewUser(user string, ctx *gin.Context) *SocketPool {
	// conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	pool := &SocketPool{
		User: user,
		Pool: queue.New(100),
	}
	sockets.pool[user] = pool
	return pool
}

// Get returns Socket for given user
func (sockets *Sockets) Get(user string, ctx *gin.Context) *SocketPool {
	if pool, ok := sockets.pool[user]; ok {
		return pool
	}
	return sockets.RegistNewUser(user, ctx)
}

func (sockets *Sockets) Remove(user string) {
	pool, ok := sockets.pool[user]
	if !ok {
		return
	}
	for !pool.Pool.Empty() {
		pool.Get()
	}
}

func (pool *SocketPool) Add(res http.ResponseWriter, req *http.Request) (*Socket, error) {
	conn, err := upgrader.Upgrade(res, req, nil)
	if err != nil {
		return nil, err
	}
	socket := &Socket{
		Conn: conn,
	}
	pool.Pool.Put(socket)
	return socket, nil
}

func (pool *SocketPool) Get() (*Socket, error) {
	item, err := pool.Pool.Get(1)
	if err != nil {
		return nil, err
	}
	sock, ok := item[0].(*Socket)
	if !ok {
		return nil, fmt.Errorf("Fail to get connection")
	}
	return sock, nil
}

// Proxy proxy request
func (pool *SocketPool) Proxy(ctx *gin.Context) {
	var err error
	var sock *Socket
	var data []byte
	sock, err = pool.Get()
	defer sock.Wait.Done()
	if err != nil {
		ctx.Error(err)
		return
	}
	data, err = EncodeRequest(ctx.Request)
	if err != nil {
		ctx.Error(err)
		return
	}
	sock.Conn.WriteMessage(websocket.BinaryMessage, data)
	_, data, err = sock.Conn.ReadMessage()
	if err != nil {
		ctx.Error(err)
		return
	}
	res, err := DecodeResponse(data)
	if err != nil {
		ctx.Error(err)
		return
	}
	ctx.Data(res.StatusCode, res.Header.Get("Content-Type"), res.Body)
}

func (pool *SocketPool) Tunnel(ctx *gin.Context) {
	var err error
	var data []byte
	var sock *Socket
	sock, err = pool.Get()
	defer sock.Wait.Done()
	if err != nil {
		ctx.Error(err)
		return
	}
	data, err = EncodeRequest(ctx.Request)
	if err != nil {
		ctx.Error(err)
		return
	}
	sock.Conn.WriteMessage(websocket.BinaryMessage, data)
	reqCtx, cancel := context.WithCancel(ctx.Request.Context())
	defer cancel()
	ctx.Writer.WriteHeader(http.StatusOK)
	ctx.Writer.WriteString("'HTTP/1.1 200 Connection Established\r\n\r\n'")
	ctx.Writer.Flush()
	hj, _ := ctx.Writer.(http.Hijacker)
	clientConn, _, _ := hj.Hijack()
	defer clientConn.Close()
	go func() {
		for {
			mt, r, err := sock.Conn.NextReader()
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return
			}
			if err != nil {
				log.Println(err)
				return
			}
			if mt != websocket.BinaryMessage {
				log.Println("blah")
				return
			}
			if _, err := io.Copy(clientConn, r); err != nil {
				log.Printf("Reading from websocket: %v", err)
				cancel()
			}
		}
	}()
	go func() {
		for {
			select {
			case <-reqCtx.Done():
				sock.Conn.Close()
				return
			default:
				continue
			}
		}
	}()
	if err := huproxy.File2WS(reqCtx, cancel, clientConn, sock.Conn); err == io.EOF {
		log.Panicln(err)
	} else if err != nil {
		log.Printf("reading from stdin: %v", err)
		cancel()
	}
}

func EncodeRequest(req *http.Request) ([]byte, error) {
	var network bytes.Buffer
	enc := gob.NewEncoder(&network)
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	newReq := Request{
		URL:    req.URL,
		Method: req.Method,
		Header: req.Header,
		Body:   body,
	}
	if err := enc.Encode(&newReq); err != nil {
		return nil, err
	}
	return network.Bytes(), nil
}

func DecodeRequest(data []byte) (*Request, error) {
	var network bytes.Buffer
	var req Request
	dec := gob.NewDecoder(&network)
	network.Write(data)
	if err := dec.Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func EncodeResponse(res *http.Response) ([]byte, error) {
	var network bytes.Buffer
	enc := gob.NewEncoder(&network)
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	newRes := Response{
		Status:     res.Status,
		StatusCode: res.StatusCode,
		Header:     res.Header,
		Body:       body,
	}
	if err := enc.Encode(&newRes); err != nil {
		return nil, err
	}
	return network.Bytes(), nil
}

func DecodeResponse(data []byte) (*Response, error) {
	var network bytes.Buffer
	var res Response
	dec := gob.NewDecoder(&network)
	network.Write(data)
	if err := dec.Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	io.Copy(destination, source)
}
