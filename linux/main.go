package main

import (
	"flag"
	"l5proxy_cv/config"
	"l5proxy_cv/xy"
	"os"
	"os/signal"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
)

func main() {
	// for debug
	var configFile string
	flag.StringVar(&configFile, "c", "", "Config file path")
	flag.Parse()

	cfg, err := config.ParseConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}

	logLevel := log.InfoLevel
	switch strings.ToLower(cfg.Server.LogLevel) {
	case "debug":
		logLevel = log.DebugLevel
	case "info":
		logLevel = log.InfoLevel
	case "warn":
		logLevel = log.WarnLevel
	case "error":
		logLevel = log.ErrorLevel
	}
	log.SetLevel(logLevel)

	if cfg.TunMode.Enabled {
		var tunName = cfg.TunMode.Device
		fd, err := openTun(tunName)
		if err != nil {
			log.Fatal(err)
		}

		cfg.TunMode.FD = fd
	}

	err = xy.Singleton().Startup(cfg)
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
