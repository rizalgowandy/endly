package main

import (
	"flag"
	"github.com/viant/endly/example/rt/elogger"
	"github.com/viant/endly/model/location"
	"log"
)

var configURI = flag.String("config", "", "path to json config file")

func main() {
	flag.Parse()
	config := &elogger.Config{}

	configResource := location.NewResource(*configURI)
	err := configResource.Decode(config)
	if err != nil {
		log.Fatal(err)
	}

	service, err := elogger.NewService(config)
	if err != nil {
		log.Fatal(err)
	}
	server, err := elogger.NewServer(config, service)
	if err != nil {
		log.Fatal(err)
	}
	server.Start()
}
