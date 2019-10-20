package routes

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	s "github.com/blueworrybear/benno-proxy/sockets"
	"github.com/gin-gonic/gin"
)

var sockets *s.Sockets

// RegisterRoutes routes routes to Gin
func RegisterRoutes(r *gin.Engine) {
	initSockets()
	r.Use(ProxyUrlRewrite(r))
	r.Any("/proxy/*path", userHTTPProxy)
	r.GET("/socket/:name", openSocket)
	r.DELETE("/socket/:name", closeSocket)
}

func initSockets() {
	sockets = s.NewSockets()
}

func handleRoot(ctx *gin.Context) {
	if hasProxyAuthorization(ctx.Request) {
		userHTTPProxy(ctx)
	} else {
		ctx.JSON(200, gin.H{
			"message": "welcome!",
		})
	}
}

func openSocket(ctx *gin.Context) {
	user := ctx.Param("name")
	pool := sockets.Get(user, ctx)
	sock, err := pool.Add(ctx.Writer, ctx.Request)
	if err != nil {
		ctx.Error(err)
		return
	}
	defer sock.Conn.Close()
	sock.Wait.Add(1)
	sock.Wait.Wait()
	fmt.Println("Fin")
}

func closeSocket(ctx *gin.Context) {
	user := ctx.Param("name")
	sockets.Remove(user)
	ctx.JSON(200, gin.H{
		"message": "ok",
	})
}

func userHTTPProxy(ctx *gin.Context) {
	user, err := proxyUser(ctx.Request)
	if err != nil {
		ctx.Error(err)
	}
	sock := sockets.Get(user, ctx)
	if ctx.Request.Method == "CONNECT" {
		sock.Tunnel(ctx)
	} else {
		sock.Proxy(ctx)
	}
}

func hasProxyAuthorization(req *http.Request) bool {
	auth := req.Header.Get("Proxy-Authorization")
	if auth == "" {
		return false
	}
	return true
}

func proxyUser(req *http.Request) (string, error) {
	user, _, ok := proxyAuth(req)
	if !ok {
		return "", fmt.Errorf("Invalid Authorization")
	}
	return user, nil
}

func proxyAuth(req *http.Request) (username, password string, ok bool) {
	const prefix = "Basic "
	auth := req.Header.Get("Proxy-Authorization")
	if auth == "" {
		return
	}
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return
	}
	c, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return
	}
	cs := string(c)
	s := strings.IndexByte(cs, ':')
	if s < 0 {
		return
	}
	return cs[:s], cs[s+1:], true
}

func ProxyUrlRewrite(r *gin.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		if hasProxyAuthorization(c.Request) {
			userHTTPProxy(c)
			c.Done()
			return
		}
		c.Request.Header.Del("Proxy")
		c.Next()
	}
}
