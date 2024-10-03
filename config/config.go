package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Config represents the structure of the TOML file
type Config struct {
	Server Server `toml:"server"`
	Tunnel Tunnel `toml:"tunnel"`
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

func ParseConfig(filePath string) (*Config, error) {
	if len(filePath) == 0 {
		return nil, fmt.Errorf("Config file path can not empty")
	}
	var config Config

	// Read and decode the TOML file
	if _, err := toml.DecodeFile(filePath, &config); err != nil {
		return nil, err
	}
	return &config, nil
}
