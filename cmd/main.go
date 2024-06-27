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
	"github.com/warthog618/sms/encoding/ucs2"
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
	if p.Header().ID != pdu.DeliverSMID {
		return
	}

	f := p.Fields()
	src := f[pdufield.SourceAddr].String()
	dst := f[pdufield.DestinationAddr].String()
	shortMessage := f[pdufield.ShortMessage].Bytes()

	// Check the PDU header ID to determine the message type
	pduID := p.Header().ID

	// Decode the short message based on PDU ID and DCS
	var txt string
	switch pduID {
	case pdu.DeliverSMID:
		// Extract the DCS (Data Coding Scheme)
		dcs := f[pdufield.DataCoding].Bytes()[0]
		txt = decodeShortMessage(shortMessage, dcs)
	default:
		logInstance.ErrorLogger.Error(fmt.Sprintf("Unsupported PDU ID: %d", pduID))
		return
	}

	message := fmt.Sprintf("Received DeliverSM from=%s to=%s: %s", src, dst, txt)
	logInstance.InfoLogger.Info(message)

	// Determine the exchange and routing key based on dst range
	var queueName, routingKey string
	if dst >= "0500" && dst <= "0555" {
		queueName = "extra.turkmentv"
		routingKey = "extra_key"
	} else {
		queueName = "sms.turkmentv"
		routingKey = "sms_key"
	}

	err := rabbitMQ.Publish(queueName, routingKey, src, dst, txt)
	if err != nil {
		logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ (%s): %v", queueName, err))
	}
}

func decodeShortMessage(shortMessage []byte, dcs byte) string {
	var txt string
	switch dcs {
	case 0x00:
		txt = string(shortMessage) // Default encoding (7-bit)
	case 0x08:
		// UCS2 encoding (16-bit)
		ucs2Text, err := ucs2.Decode(shortMessage)
		if err != nil {
			logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to decode UCS2 message: %v", err))
			return ""
		}
		txt = string(ucs2Text)
	default:
		logInstance.ErrorLogger.Error(fmt.Sprintf("Unsupported DCS: %d", dcs))
		return ""
	}
	return txt
}
