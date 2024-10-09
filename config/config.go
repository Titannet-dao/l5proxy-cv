package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Config represents the structure of the TOML file
type Config struct {
	Server Server `toml:"server"`
	Tunnel Tunnel `toml:"tunnel"`

	HTTPMode   HTTPMode   `toml:"http"`
	Socks5Mode Socks5Mode `toml:"socks5"`
	TunMode    TunMode    `toml:"tun"`
	BypassMode BypassMode `toml:"bypass"`
}

type Server struct {
	URL      string `toml:"url"`
	UUID     string `toml:"uuid"`
	Endpiont string `toml:"endpoint"`
	Mark     int    `toml:"mark"`
	LogLevel string `toml:"loglevel"`
}

type Tunnel struct {
	Count int `toml:"count"`
	Cap   int `toml:"cap"`
}

type BypassMode struct {
	Enabled      bool   `toml:"enabled"`
	WhitelistURL string `toml:"whitelist"`
}

type HTTPMode struct {
	Enabled bool   `toml:"enabled"`
	Address string `toml:"address"`
	Bypass  bool   `toml:"bypass"`
}

type Socks5Mode struct {
	Enabled bool   `toml:"enabled"`
	Address string `toml:"address"`
	Bypass  bool   `toml:"bypass"`
}

type TunMode struct {
	Enabled bool   `toml:"enabled"`
	Bypass  bool   `toml:"bypass"`
	Device  string `toml:"dev"`
	MTU     uint32 `toml:"mtu"`
	FD      int
}

func ParseConfig(filePath string) (*Config, error) {
	if len(filePath) == 0 {
		return nil, fmt.Errorf("Config file path can not empty")
	}
	var config Config

	// Read and decode the TOML file
	if _, err := toml.DecodeFile(filePath, &config); err != nil {
		return nil, err
	}

	if config.Server.UUID == "" {
		return nil, fmt.Errorf("Config must have an UUID")
	}

	if config.Server.URL == "" {
		return nil, fmt.Errorf("Config must have a websocket URL")
	}

	if config.TunMode.Enabled {
		if config.TunMode.Device == "" {
			config.TunMode.Device = "tun0xy"
		}

		if config.TunMode.MTU == 0 {
			config.TunMode.MTU = 1500
		}
	}

	if config.Tunnel.Cap > 200 {
		config.Tunnel.Cap = 200
	}

	if config.Tunnel.Cap < 50 {
		config.Tunnel.Cap = 50
	}

	if config.Tunnel.Count > 20 {
		config.Tunnel.Count = 20
	}

	if config.Tunnel.Count < 1 {
		config.Tunnel.Count = 3
	}

	return &config, nil
}
