package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Config represents the structure of the TOML file
type Config struct {
	Server Server `toml:"server"`
	Tun    Tun    `toml:"tun"`
}

type Server struct {
	URL string `toml:"url"`
}

type Tun struct {
	Count int `toml:"count"`
	Cap   int `toml:"cap"`
}

type Log struct {
	Level string `toml:"level"`
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
