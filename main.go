package main

import (
	"log"
	"os"

	"github.com/blueworrybear/benno-proxy/cmd"
	"github.com/urfave/cli"
)

func main() {
	app := cli.NewApp()
	app.Name = "Benno Proxy"
	app.Commands = []cli.Command{
		cmd.CmdServer,
		cmd.CmdClient,
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatalf("Failed to run app with %s: %v", os.Args, err)
	}
}
