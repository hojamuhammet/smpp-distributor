package main

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"smpp-distributor/internal/config"
	"smpp-distributor/pkg/logger"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/fiorix/go-smpp/smpp"
	"github.com/fiorix/go-smpp/smpp/pdu"
	"github.com/fiorix/go-smpp/smpp/pdu/pdufield"
)

var logInstance *logger.Loggers

func main() {
	cfg := config.LoadConfig()

	var err error
	logInstance, err = logger.SetupLogger(cfg.Env)
	if err != nil {
		slog.Error("failed to set up logger: %v", err)
		os.Exit(1)
	}

	logInstance.InfoLogger.Info("Server is up and running")
	slog.Info("Server is up and running")

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
	switch p.Header().ID {
	case pdu.DeliverSMID:
		f := p.Fields()
		src := f[pdufield.SourceAddr]
		dst := f[pdufield.DestinationAddr]
		txt := f[pdufield.ShortMessage]

		// Convert the UCS-2 encoded message to UTF-16
		ucs2Message := txt.Bytes()
		utf16Message := make([]uint16, len(ucs2Message)/2)
		for i := range utf16Message {
			utf16Message[i] = binary.BigEndian.Uint16(ucs2Message[i*2 : (i+1)*2])
		}
		decodedMessage := string(utf16.Decode(utf16Message))

		// If the decoded message is not valid UTF-8 or contains non-Latin characters,
		// decode it as ASCII or UTF-8 instead
		if !utf8.ValidString(decodedMessage) || strings.ContainsAny(decodedMessage, "䥫楮摩渠慲愠摩祩瀠慴污湤祲祬祡爠\u2061瑡\u2062慢慬慲浹穹渠獡条琠摩祰⁵污湡渠穡摹") {
			decodedMessage = string(txt.Bytes())
		}

		logInstance.InfoLogger.Info(fmt.Sprintf("Received message from=%s to=%s: %s", src.String(), dst.String(), decodedMessage))
	default:
		fmt.Println("Received PDU:", p)
	}
}
