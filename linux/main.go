package main

import (
	"flag"
	"lproxy_tun/config"
	"lproxy_tun/xy"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
)

func main() {
	// for debug
	var configFile string
	var tunName string
	flag.StringVar(&configFile, "c", "", "Config file path")
	flag.StringVar(&tunName, "tun", "tun0xy", "tun name")
	flag.Parse()

	cfg, err := config.ParseConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}

	log.SetLevel(log.DebugLevel)

	err = xy.Singleton().Startup(tunName, 1500, cfg)
	if err != nil {
		log.Fatal(err)
	}

	defer func() {
		err = xy.Singleton().Shutdown()
		if err != nil {
			log.Fatal(err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
