package main

import (
	"os"

	"log/slog"
	"smpp-distributor/internal/config"
	rabbitmq "smpp-distributor/internal/infrastructure/rabbitmq"
	"smpp-distributor/internal/infrastructure/smpp"
	"smpp-distributor/pkg/logger"
	"smpp-distributor/pkg/utils"
)

var (
	logInstance     *logger.Loggers
	rabbitMQ        *rabbitmq.RabbitMQManager
	onCloseRabbitMQ = make(chan bool)
)

func main() {
	cfg := config.LoadConfig()

	var err error
	logInstance, err = logger.SetupLogger(cfg.Env)
	if err != nil {
		slog.Error("failed to set up logger: %v", utils.Err(err))
		os.Exit(1)
	}

	logInstance.InfoLogger.Info("Server is up and running")

	// Initialize RabbitMQ
	rabbitMQ, err = rabbitmq.NewRabbitMQManager(cfg.RabbitMQ, logInstance, onCloseRabbitMQ)
	if err != nil {
		logInstance.ErrorLogger.Error("failed to set up RabbitMQ: %v", utils.Err(err))
		os.Exit(1)
	}
	defer rabbitMQ.Close()

	// Initialize SMPP handler
	smppHandler := smpp.NewSMPPHandler(cfg, logInstance, rabbitMQ, onCloseRabbitMQ)

	go func() {
		for {
			status := <-onCloseRabbitMQ
			if status {
				logInstance.InfoLogger.Info("RabbitMQ connection lost, stopping SMPP receiver")
				if smppHandler.SmppReceiver != nil {
					smppHandler.SmppReceiver.Close()
				}
			} else {
				logInstance.InfoLogger.Info("RabbitMQ reconnected, starting SMPP receiver")
				go smppHandler.ConnectToSMPP(cfg)
			}
		}
	}()

	go smppHandler.ConnectToSMPP(cfg)

	select {}
}
