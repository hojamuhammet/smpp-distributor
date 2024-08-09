package config

import (
	"log"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env       string    `yaml:"env"`
	RabbitMQ  RabbitMQ  `yaml:"rabbitmq"`
	SMPP      SMPP      `yaml:"smpp"`
	WebSocket WebSocket `yaml:"websocket"`
}

type RabbitMQ struct {
	URL        string               `yaml:"url"`
	Publishers map[string]Publisher `yaml:"publishers"`
}

type Publisher struct {
	ExchangeName string `yaml:"exchange_name"`
	QueueName    string `yaml:"queue_name"`
	RoutingKey   string `yaml:"routing_key"`
}

type SMPP struct {
	Addr string `yaml:"address"`
	User string `yaml:"user"`
	Pass string `yaml:"password"`
}

type WebSocket struct {
	Addr string `yaml:"address"`
}

func LoadConfig() *Config {
	configPath := "config.yaml"

	if configPath == "" {
		log.Fatalf("config path is not set or config file does not exist")
	}

	var cfg Config

	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		log.Fatalf("Cannot read config: %v", err)
	}

	return &cfg
}
