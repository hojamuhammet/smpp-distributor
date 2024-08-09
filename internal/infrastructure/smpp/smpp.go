package smpp

import (
	"fmt"
	"sync"
	"time"

	"smpp-distributor/internal/config"
	"smpp-distributor/internal/infrastructure/rabbitmq"
	"smpp-distributor/pkg/logger"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu"
	"github.com/fiorix/go-smpp/smpp/pdu/pdufield"
	"github.com/warthog618/sms/encoding/ucs2"
)

type MessagePart struct {
	TotalParts  byte
	CurrentPart byte
	Content     []byte
}

type SMPPHandler struct {
	LogInstance  *logger.Loggers
	RabbitMQ     *rabbitmq.RabbitMQManager
	OnClose      chan bool
	SmppReceiver *smpp.Receiver
	MessageStore map[string]map[byte]MessagePart
	Mu           sync.RWMutex
}

func NewSMPPHandler(cfg *config.Config, logInstance *logger.Loggers, rabbitMQ *rabbitmq.RabbitMQManager, onClose chan bool) *SMPPHandler {
	return &SMPPHandler{
		LogInstance:  logInstance,
		RabbitMQ:     rabbitMQ,
		OnClose:      onClose,
		MessageStore: make(map[string]map[byte]MessagePart),
	}
}

func (s *SMPPHandler) ConnectToSMPP(cfg *config.Config) {
	s.SmppReceiver = &smpp.Receiver{
		Addr:    cfg.SMPP.Addr,
		User:    cfg.SMPP.User,
		Passwd:  cfg.SMPP.Pass,
		Handler: s.handlerFunc,
	}

	connStatus := make(chan smpp.ConnStatusID)
	go func() {
		for c := range s.SmppReceiver.Bind() {
			connStatus <- c.Status()
		}
	}()

	for {
		status := <-connStatus
		if status == smpp.Connected {
			s.LogInstance.InfoLogger.Info("SMPP connection established")
			return
		}
		s.LogInstance.InfoLogger.Info("SMPP connection status: " + status.String())
		if status == smpp.Disconnected {
			s.LogInstance.ErrorLogger.Error("SMPP connection failed")
			time.Sleep(5 * time.Second)
			go func() {
				for c := range s.SmppReceiver.Bind() {
					connStatus <- c.Status()
				}
			}()
		}
	}
}

func (s *SMPPHandler) handlerFunc(p pdu.Body) {
	if p.Header().ID != pdu.DeliverSMID {
		return
	}

	fields := p.Fields()
	src := fields[pdufield.SourceAddr].String()
	dst := fields[pdufield.DestinationAddr].String()
	shortMessage := fields[pdufield.ShortMessage].Bytes()
	dcs := fields[pdufield.DataCoding].Bytes()[0]
	date := time.Now().Format("2006-01-02T15:04:05")

	if fields[pdufield.UDHLength] != nil {
		s.handleMultipartMessage(src, dst, shortMessage, fields[pdufield.GSMUserData].Bytes(), dcs, date)
	} else {
		txt := s.decodeShortMessage(shortMessage, dcs)
		s.logAndPublishMessage(src, dst, txt, date, 1)
	}
}

func (s *SMPPHandler) handleMultipartMessage(src, dst string, shortMessage, gsmUserData []byte, dcs byte, date string) {
	refNum := fmt.Sprintf("%x", gsmUserData[2])
	totalParts := gsmUserData[3]
	currentPart := gsmUserData[4]

	s.Mu.Lock()
	if _, exists := s.MessageStore[refNum]; !exists {
		s.MessageStore[refNum] = make(map[byte]MessagePart)
	}
	s.MessageStore[refNum][currentPart] = MessagePart{
		TotalParts:  totalParts,
		CurrentPart: currentPart,
		Content:     shortMessage,
	}
	s.Mu.Unlock()

	if len(s.MessageStore[refNum]) == int(totalParts) {
		var fullMessage []byte
		for i := byte(1); i <= totalParts; i++ {
			fullMessage = append(fullMessage, s.MessageStore[refNum][i].Content...)
		}
		s.Mu.Lock()
		delete(s.MessageStore, refNum)
		s.Mu.Unlock()

		txt := s.decodeShortMessage(fullMessage, dcs)
		s.logAndPublishMessage(src, dst, txt, date, int(totalParts))
	}
}

func (s *SMPPHandler) logAndPublishMessage(src, dst, txt, date string, parts int) {
	message := fmt.Sprintf("Reassembled message from=%s to=%s: %s, date=%s, parts=%d", src, dst, txt, date, parts)
	s.LogInstance.InfoLogger.Info(message)

	err := s.RabbitMQ.Publish("extra.turkmentv", src, dst, txt, date, parts)
	if err != nil {
		s.LogInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ (extra.turkmentv): %v", err))
	} else {
		s.LogInstance.InfoLogger.Info(fmt.Sprintf("Message published to RabbitMQ (extra.turkmentv): %s", message))
	}

	err = s.RabbitMQ.Publish("sms.turkmentv", src, dst, txt, date, parts)
	if err != nil {
		s.LogInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ (sms.turkmentv): %v", err))
	} else {
		s.LogInstance.InfoLogger.Info(fmt.Sprintf("Message published to RabbitMQ (sms.turkmentv): %s", message))
	}
}

func (s *SMPPHandler) decodeShortMessage(shortMessage []byte, dcs byte) string {
	var txt string
	switch dcs {
	case 0x00:
		txt = string(shortMessage)
	case 0x08:
		ucs2Text, err := ucs2.Decode(shortMessage)
		if err != nil {
			s.LogInstance.ErrorLogger.Error(fmt.Sprintf("Failed to decode UCS2 message: %v", err))
			return ""
		}
		txt = string(ucs2Text)
	default:
		s.LogInstance.ErrorLogger.Error(fmt.Sprintf("Unsupported DCS: %d", dcs))
		return ""
	}
	return txt
}
