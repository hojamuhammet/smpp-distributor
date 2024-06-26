package config

import (
	"log"

	"smpp-distributor/pkg/utils"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env      string `yaml:"env"`
	SMPP     `yaml:"smpp"`
	RabbitMQ RabbitMQ `yaml:"rabbitmq"`
}

type SMPP struct {
	Addr string `yaml:"address"`
	User string `yaml:"user"`
	Pass string `yaml:"password"`
}

type RabbitMQ struct {
	URL string `yaml:"url"`
}

func LoadConfig() *Config {
	configPath := "config.yaml"

	if configPath == "" {
		log.Fatalf("config path is not set or config file does not exist")
	}

	var cfg Config

	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatalf("Cannot read config: %v", utils.Err(err))
	}

	return &cfg
}
