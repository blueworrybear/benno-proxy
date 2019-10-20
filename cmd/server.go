package cmd

import (
	"github.com/blueworrybear/benno-proxy/routes"
	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
)

// CmdServer runs server command
var CmdServer = cli.Command{
	Name:   "server",
	Action: runServer,
}

func runServer(ctx *cli.Context) {
	r := gin.Default()
	routes.RegisterRoutes(r)
	r.Run()
}
