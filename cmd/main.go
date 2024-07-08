package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"smpp-distributor/internal/config"
	rabbitmq "smpp-distributor/internal/infrastructure"
	"smpp-distributor/pkg/logger"
	"smpp-distributor/pkg/utils"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu"
	"github.com/fiorix/go-smpp/smpp/pdu/pdufield"
	"github.com/warthog618/sms/encoding/ucs2"
)

var (
	logInstance *logger.Loggers
	rabbitMQ    *rabbitmq.RabbitMQ
)

type MessageParts struct {
	TotalParts int
	Parts      map[int][]byte
	Received   int
	LastUpdate time.Time
}

type MessagePart struct {
	TotalParts  byte
	CurrentPart byte
	Content     []byte
}

var messageStore = struct {
	sync.RWMutex
	store map[string]map[byte]MessagePart
}{
	store: make(map[string]map[byte]MessagePart),
}

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
	rabbitMQ, err = rabbitmq.NewRabbitMQ(cfg.RabbitMQ, logInstance)
	if err != nil {
		logInstance.ErrorLogger.Error("failed to set up RabbitMQ: %v", utils.Err(err))
		os.Exit(1)
	}
	defer rabbitMQ.Close()

	for {
		if connectToSMPP(*cfg) {
			break
		}
		logInstance.InfoLogger.Info("Retrying SMPP connection in 5 seconds...")
		time.Sleep(5 * time.Second)
	}

	select {}
}

func connectToSMPP(cfg config.Config) bool {
	r := &smpp.Receiver{
		Addr:    cfg.SMPP.Addr,
		User:    cfg.SMPP.User,
		Passwd:  cfg.SMPP.Pass,
		Handler: handlerFunc,
	}

	connStatus := make(chan smpp.ConnStatusID)
	go func() {
		for c := range r.Bind() {
			connStatus <- c.Status()
		}
	}()

	for {
		status := <-connStatus
		if status == smpp.Connected {
			logInstance.InfoLogger.Info("SMPP connection established")
			return true
		}
		logInstance.InfoLogger.Info("SMPP connection status: " + status.String())
		if status == smpp.Disconnected {
			logInstance.ErrorLogger.Error("SMPP connection failed")
			return false
		}
	}
}

func handlerFunc(p pdu.Body) {
	if p.Header().ID != pdu.DeliverSMID {
		return
	}

	fields := p.Fields()
	src := fields[pdufield.SourceAddr].String()
	dst := fields[pdufield.DestinationAddr].String()
	shortMessage := fields[pdufield.ShortMessage].Bytes()
	dcs := fields[pdufield.DataCoding].Bytes()[0]
	date := time.Now().Format("2006-01-02T15:04:05")

	// Check if the message is segmented by looking for UDH (User Data Header)
	if fields[pdufield.UDHLength] != nil {
		handleMultipartMessage(src, dst, shortMessage, fields[pdufield.GSMUserData].Bytes(), dcs, date)
	} else {
		txt := decodeShortMessage(shortMessage, dcs)
		logAndPublishMessage(src, dst, txt, date, 1) // Single part message
	}
}

func handleMultipartMessage(src, dst string, shortMessage, gsmUserData []byte, dcs byte, date string) {
	// Parse the UDH to get the message reference number, total parts, and current part number
	refNum := fmt.Sprintf("%x", gsmUserData[2])
	totalParts := gsmUserData[3]
	currentPart := gsmUserData[4]

	// Store the message part
	messageStore.Lock()
	if _, exists := messageStore.store[refNum]; !exists {
		messageStore.store[refNum] = make(map[byte]MessagePart)
	}
	messageStore.store[refNum][currentPart] = MessagePart{
		TotalParts:  totalParts,
		CurrentPart: currentPart,
		Content:     shortMessage,
	}
	messageStore.Unlock()

	// Check if all parts are received
	if len(messageStore.store[refNum]) == int(totalParts) {
		var fullMessage []byte
		for i := byte(1); i <= totalParts; i++ {
			fullMessage = append(fullMessage, messageStore.store[refNum][i].Content...)
		}
		messageStore.Lock()
		delete(messageStore.store, refNum)
		messageStore.Unlock()

		// Decode and log the complete message
		txt := decodeShortMessage(fullMessage, dcs)
		logAndPublishMessage(src, dst, txt, date, int(totalParts)) // Reassembled message parts count
	}
}

func logAndPublishMessage(src, dst, txt, date string, parts int) {
	message := fmt.Sprintf("Reassembled message from=%s to=%s: %s, date=%s, parts=%d", src, dst, txt, date, parts)
	logInstance.InfoLogger.Info(message)

	// Publish the message to both queues
	err := rabbitMQ.Publish("extra.turkmentv", src, dst, txt, date, parts)
	if err != nil {
		logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ (extra.turkmentv): %v", err))
	} else {
		logInstance.InfoLogger.Info(fmt.Sprintf("Message published to RabbitMQ (extra.turkmentv): %s", message))
	}

	err = rabbitMQ.Publish("sms.turkmentv", src, dst, txt, date, parts)
	if err != nil {
		logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ (sms.turkmentv): %v", err))
	} else {
		logInstance.InfoLogger.Info(fmt.Sprintf("Message published to RabbitMQ (sms.turkmentv): %s", message))
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
