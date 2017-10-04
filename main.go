package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/bjhaid/oga/initializer"
	"github.com/bjhaid/oga/requester"

	"github.com/golang/glog"
)

func main() {
	flag.Set("logtostderr", "true")
	flag.Set("v", "2")
	flag.Parse()
	defer glog.Flush()

	stop := make(chan struct{})
	c := make(chan os.Signal, 1)
	req := requester.NewSlackRequester()

	initializer.Run(stop, req)
	req.Run()

	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-c:
		stop <- struct{}{}
		glog.Info("Shutting down")
		os.Exit(0)
	}
}
