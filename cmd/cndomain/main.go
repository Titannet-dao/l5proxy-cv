package main

import (
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
)

func main() {
	rsp, err := http.Get("https://raw.githubusercontent.com/felixonmars/dnsmasq-china-list/refs/heads/master/accelerated-domains.china.conf")
	if err != nil {
		log.Fatal(err)
	}

	defer rsp.Body.Close()

	if rsp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(rsp.Body)
		if err != nil {
			log.Fatal(err)
		}
		bodyString := string(bodyBytes)
		log.Info(bodyString)
	} else {
		log.Fatalf("rsp status code %d != 200", rsp.StatusCode)
	}
}
