package main

import (
	"flag"
	"fmt"
	"l5proxy_cv/config"
	"l5proxy_cv/xy"
	"os"
	"os/signal"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/link/tun"
)

func openTun(name string) (int, error) {
	if len(name) >= unix.IFNAMSIZ {
		return -1, fmt.Errorf("interface name too long: %s", name)
	}

	fd, err := tun.Open(name)
	if err != nil {
		return -1, fmt.Errorf("create tun: %w", err)
	}

	return fd, nil
}

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

	logLevel := log.InfoLevel
	switch strings.ToLower(cfg.Log.Level) {
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

	fd, err := openTun(tunName)
	if err != nil {
		log.Fatal(err)
	}

	err = xy.Singleton().Startup(fd, 1500, cfg)
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
