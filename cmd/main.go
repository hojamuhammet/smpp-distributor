package main

import (
	"fmt"
	"log/slog"
	"os"
	"smpp-distributor/internal/config"
	rabbitmq "smpp-distributor/internal/infrastructure"
	"smpp-distributor/pkg/logger"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu"
	"github.com/fiorix/go-smpp/smpp/pdu/pdufield"
)

var logInstance *logger.Loggers
var rabbitMQ *rabbitmq.RabbitMQ

func main() {
	cfg := config.LoadConfig()

	var err error
	logInstance, err = logger.SetupLogger(cfg.Env)
	if err != nil {
		slog.Error("failed to set up logger: %v", err)
		os.Exit(1)
	}

	logInstance.InfoLogger.Info("Server is up and running")

	// Initialize RabbitMQ
	rabbitMQ, err = rabbitmq.NewRabbitMQ(cfg.RabbitMQ)
	if err != nil {
		logInstance.ErrorLogger.Error("failed to set up RabbitMQ: %v", err)
		os.Exit(1)
	}
	defer rabbitMQ.Close()

	r := &smpp.Receiver{
		Addr:    cfg.SMPP.Addr,
		User:    cfg.SMPP.User,
		Passwd:  cfg.SMPP.Pass,
		Handler: handlerFunc,
	}

	go func() {
		for c := range r.Bind() {
			logInstance.InfoLogger.Info("SMPP connection status: " + c.Status().String())
		}
	}()

	select {}
}

func handlerFunc(p pdu.Body) {
	f := p.Fields()
	src := f[pdufield.SourceAddr]
	dst := f[pdufield.DestinationAddr]
	txt := f[pdufield.ShortMessage]

	message := fmt.Sprintf("Received DeliverSM from=%s to=%s: %s", src.String(), dst.String(), txt.String())

	logInstance.InfoLogger.Info(message)

	err := rabbitMQ.Publish("sms_queue", src.String(), txt.String())
	if err != nil {
		logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ: %v", err))
	}
}
